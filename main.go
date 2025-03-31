package main

import (
	"bufio"
	"bytes"
	"container/list"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"log"
	"math"
	"net"
	"runtime"
	"sync"
	"time"

	"github.com/cespare/xxhash/v2"
	"github.com/shirou/gopsutil/v3/mem"
)

const (
	maxLength    = 256
	shardCount   = 512
	ttlSeconds   = 120
	cleanupEvery = 10 * time.Second
	maxMemoryPct = 70
)

// Command types
const (
	cmdPut byte = 1
	cmdGet byte = 2
)

// Response status
const (
	statusOK    byte = 1
	statusError byte = 2
)

var (
	ErrKeyNotFound   = errors.New("key not found")
	ErrKeyTooLarge   = errors.New("key exceeds maximum length")
	ErrValueTooLarge = errors.New("value exceeds maximum length")
)

type cacheItem struct {
	key       string
	value     string
	expiresAt int64 // Unix timestamp
}

type shard struct {
	mu    sync.RWMutex
	data  map[string]*list.Element
	lru   *list.List
	count int
}

type Cache struct {
	shards    []*shard
	itemCount int64
}

func NewCache() *Cache {
	shards := make([]*shard, shardCount)
	for i := 0; i < shardCount; i++ {
		shards[i] = &shard{
			data:  make(map[string]*list.Element),
			lru:   list.New(),
			count: 0,
		}
		go shards[i].startEviction()
		go monitorMemoryUsage(shards)
	}
	return &Cache{shards: shards}
}

func getTotalMemory() uint64 {
	const defaultMemory uint64 = 2 * 1024 * 1024 * 1024

	vmStat, err := mem.VirtualMemory()
	if err != nil {
		log.Printf("Error retrieving memory info: %v", err)
		return defaultMemory
	}

	return vmStat.Total
}

func monitorMemoryUsage(shards []*shard) {
	ticker := time.NewTicker(1 * time.Second)
	defer ticker.Stop()
	log.Println("Monitoring memory usage...")

	for range ticker.C {
		var memStats runtime.MemStats
		runtime.ReadMemStats(&memStats)
		totalMem := getTotalMemory()
		threshold := totalMem * maxMemoryPct / 100
		memUsageMB := memStats.Alloc / 1024 / 1024
		thresholdMB := threshold / 1024 / 1024

		if memStats.Alloc > threshold {
			log.Printf("Memory usage Critical: %d MB used (threshold: %d MB).", memUsageMB, thresholdMB)
			evictAcrossShards(shards, threshold)
		}
	}
}

func evictAcrossShards(shards []*shard, threshold uint64) {
	// Calculate items to evict per shard
	targetEviction := 50 // Start with a small batch

	for {
		var memStats runtime.MemStats
		runtime.ReadMemStats(&memStats)
		if memStats.Alloc <= threshold/2 {
			log.Printf("Memory reduced to %d MB (below target %d MB), stopping eviction",
				memStats.Alloc/1024/1024, threshold/2/1024/1024)
			break
		}

		// Evict from each shard in a round-robin fashion
		evictedTotal := 0
		for _, s := range shards {
			evicted := s.evictBatch(targetEviction / shardCount)
			evictedTotal += evicted
			if evicted == 0 {
				// This shard has nothing to evict
				continue
			}
		}

		if evictedTotal == 0 {
			log.Println("No more items to evict")
			break
		}

		// Increase batch size for faster eviction if memory is still high
		targetEviction = int(math.Min(float64(targetEviction*2), 5000))
		runtime.GC()
		time.Sleep(50 * time.Millisecond)
	}
}

func (s *shard) evictBatch(count int) int {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.lru.Len() == 0 {
		return 0
	}

	evicted := 0
	for i := 0; i < count && s.lru.Len() > 0; i++ {
		elem := s.lru.Back()
		if elem == nil {
			break
		}

		item := elem.Value.(*cacheItem)
		s.lru.Remove(elem)
		delete(s.data, item.key)
		evicted++
		s.count--
	}

	return evicted
}

func (s *shard) startEviction() {
	ticker := time.NewTicker(cleanupEvery)
	defer ticker.Stop()
	for range ticker.C {
		s.mu.Lock()
		now := time.Now().Unix()
		toRemove := make([]*list.Element, 0)

		for _, elem := range s.data {
			item := elem.Value.(*cacheItem)
			if item.expiresAt <= now {
				toRemove = append(toRemove, elem)
			}
		}

		for _, elem := range toRemove {
			item := elem.Value.(*cacheItem)
			delete(s.data, item.key)
			s.lru.Remove(elem)
			s.count--
		}

		s.mu.Unlock()
	}
}

func (c *Cache) getShard(key string) *shard {
	hashVal := xxhash.Sum64String(key)
	index := int(hashVal & (shardCount - 1))
	return c.shards[index]
}

func (c *Cache) Put(key, value string) bool {
	if len(key) > maxLength || len(value) > maxLength {
		return false
	}

	s := c.getShard(key)
	s.mu.Lock()
	defer s.mu.Unlock()

	now := time.Now().Unix()
	expiresAt := now + ttlSeconds

	// Check if key already exists
	if elem, exists := s.data[key]; exists {
		// Update existing item
		item := elem.Value.(*cacheItem)
		item.value = value
		item.expiresAt = expiresAt
		s.lru.MoveToFront(elem)
		return true
	}

	// Create new item
	item := &cacheItem{
		key:       key,
		value:     value,
		expiresAt: expiresAt,
	}

	elem := s.lru.PushFront(item)
	s.data[key] = elem
	s.count++

	return true
}

func (c *Cache) Get(key string) (string, bool) {
	s := c.getShard(key)
	s.mu.RLock()
	elem, ok := s.data[key]

	if !ok {
		s.mu.RUnlock()
		return "", false
	}

	item := elem.Value.(*cacheItem)
	value := item.value

	// Check if expired
	now := time.Now().Unix()
	if item.expiresAt <= now {
		s.mu.RUnlock()
		// Asynchronously remove expired item
		go func() {
			s.mu.Lock()
			if elem, exists := s.data[key]; exists {
				delete(s.data, key)
				s.lru.Remove(elem)
				s.count--
			}
			s.mu.Unlock()
		}()
		return "", false
	}

	s.mu.RUnlock()

	// Update LRU position (with full lock)
	go func() {
		s.mu.Lock()
		if elem, exists := s.data[key]; exists {
			s.lru.MoveToFront(elem)
		}
		s.mu.Unlock()
	}()

	return value, true
}

func handleConnection(conn net.Conn, cache *Cache) {
	defer conn.Close()
	reader := bufio.NewReader(conn)

	for {
		// Read command byte
		cmdType, err := reader.ReadByte()
		if err != nil {
			if err != io.EOF {
				log.Printf("Error reading command: %v", err)
			}
			return
		}

		switch cmdType {
		case cmdPut:
			handlePut(reader, conn, cache)
		case cmdGet:
			handleGet(reader, conn, cache)
		default:
			// Unknown command, respond with error
			writeErrorResponse(conn, "Unknown command")
		}
	}
}

func handlePut(reader *bufio.Reader, conn net.Conn, cache *Cache) {
	// Read key length (2 bytes)
	keyLenBytes := make([]byte, 2)
	if _, err := io.ReadFull(reader, keyLenBytes); err != nil {
		writeErrorResponse(conn, "Failed to read key length")
		return
	}
	keyLen := binary.BigEndian.Uint16(keyLenBytes)

	// Read value length (2 bytes)
	valueLenBytes := make([]byte, 2)
	if _, err := io.ReadFull(reader, valueLenBytes); err != nil {
		writeErrorResponse(conn, "Failed to read value length")
		return
	}
	valueLen := binary.BigEndian.Uint16(valueLenBytes)

	// Check lengths against max
	if keyLen > maxLength || valueLen > maxLength {
		writeErrorResponse(conn, "Key or value too long")
		return
	}

	// Read key
	keyBytes := make([]byte, keyLen)
	if _, err := io.ReadFull(reader, keyBytes); err != nil {
		writeErrorResponse(conn, "Failed to read key")
		return
	}

	// Read value
	valueBytes := make([]byte, valueLen)
	if _, err := io.ReadFull(reader, valueBytes); err != nil {
		writeErrorResponse(conn, "Failed to read value")
		return
	}

	// Put in cache
	success := cache.Put(string(keyBytes), string(valueBytes))

	// Write response
	if success {
		conn.Write([]byte{statusOK})
	} else {
		writeErrorResponse(conn, "Cache full or key too long")
	}
}

func handleGet(reader *bufio.Reader, conn net.Conn, cache *Cache) {
	// Read key length (2 bytes)
	keyLenBytes := make([]byte, 2)
	if _, err := io.ReadFull(reader, keyLenBytes); err != nil {
		writeErrorResponse(conn, "Failed to read key length")
		return
	}
	keyLen := binary.BigEndian.Uint16(keyLenBytes)

	// Check length against max
	if keyLen > maxLength {
		writeErrorResponse(conn, "Key too long")
		return
	}

	// Read key
	keyBytes := make([]byte, keyLen)
	if _, err := io.ReadFull(reader, keyBytes); err != nil {
		writeErrorResponse(conn, "Failed to read key")
		return
	}

	// Get from cache
	value, found := cache.Get(string(keyBytes))
	if !found {
		value = "-1"
	}

	// Write success response with value
	valueBytes := []byte(value)
	responseBuffer := &bytes.Buffer{}
	responseBuffer.WriteByte(statusOK)

	// Write value length (2 bytes)
	valueLenBytes := make([]byte, 2)
	binary.BigEndian.PutUint16(valueLenBytes, uint16(len(valueBytes)))
	responseBuffer.Write(valueLenBytes)

	// Write value
	responseBuffer.Write(valueBytes)

	conn.Write(responseBuffer.Bytes())
}

func writeErrorResponse(conn net.Conn, message string) {
	messageBytes := []byte(message)
	responseBuffer := &bytes.Buffer{}

	responseBuffer.WriteByte(statusError)

	// Write message length (2 bytes)
	msgLenBytes := make([]byte, 2)
	binary.BigEndian.PutUint16(msgLenBytes, uint16(len(messageBytes)))
	responseBuffer.Write(msgLenBytes)

	// Write message
	responseBuffer.Write(messageBytes)

	conn.Write(responseBuffer.Bytes())
}

func main() {
	runtime.GOMAXPROCS(runtime.NumCPU())
	cache := NewCache()

	listener, err := net.Listen("tcp", ":7171") // Listen on port 7171 as required
	if err != nil {
		log.Fatalf("Failed to start server: %v", err)
	}
	defer listener.Close()

	fmt.Println("Server listening on port 7171...")

	for {
		conn, err := listener.Accept()
		if err != nil {
			log.Printf("Error accepting connection: %v", err)
			continue
		}
		go handleConnection(conn, cache)
	}
}

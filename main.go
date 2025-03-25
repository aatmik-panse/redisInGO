package main

import (
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"runtime"
	"sync"
	"sync/atomic"
	"time"

	"github.com/cespare/xxhash/v2"
)

const (
	maxLength  = 256
	shardCount = 64  // Must be a power of 2 to use bitmasking
	logBuffer  = 100 // Log buffer size to avoid blocking
)

// shard represents one partition of the cache.
type shard struct {
	mu     sync.RWMutex
	data   map[string]string
	hits   int64
	misses int64
	access int64
}

// Cache is a sharded in-memory key-value store.
type Cache struct {
	shards []*shard
	stats  struct {
		startTime time.Time
		putCount  int64
		getCount  int64
	}
}

/* // Logger handles non-blocking logging using a buffered channel.
type Logger struct {
	logChan chan string
}

// NewLogger initializes the logger with a background worker.
func NewLogger() *Logger {
	l := &Logger{
		logChan: make(chan string, logBuffer),
	}
	go l.processLogs()
	return l
}

// processLogs continuously writes logs without blocking the main execution.
func (l *Logger) processLogs() {
	for msg := range l.logChan {
		log.Println(msg)
	}
}

// Log sends a message to the log channel.
func (l *Logger) Log(format string, args ...interface{}) {
	select {
	case l.logChan <- fmt.Sprintf(format, args...):
	default:
		// Drop log if the channel is full to prevent blocking
	}
} */

// NewCache initializes the cache with a fixed number of shards.
func NewCache() *Cache {
	shards := make([]*shard, shardCount)
	for i := 0; i < shardCount; i++ {
		shards[i] = &shard{
			data: make(map[string]string),
		}
	}

	cache := &Cache{
		shards: shards,
	}
	cache.stats.startTime = time.Now()

	return cache
}

// getShard returns the shard responsible for the given key.
func (c *Cache) getShard(key string) *shard {
	hashVal := xxhash.Sum64String(key)
	index := int(hashVal & (shardCount - 1))
	return c.shards[index]
}

// Put inserts or updates the key-value pair in the cache.
func (c *Cache) Put(key, value string) {
	s := c.getShard(key)
	s.mu.Lock()
	s.data[key] = value
	s.mu.Unlock()

	atomic.AddInt64(&c.stats.putCount, 1)
}

// Get retrieves the value for a given key.
func (c *Cache) Get(key string) (string, bool) {
	s := c.getShard(key)
	s.mu.RLock()
	atomic.AddInt64(&s.access, 1)
	value, ok := s.data[key]

	if ok {
		atomic.AddInt64(&s.hits, 1)
	} else {
		atomic.AddInt64(&s.misses, 1)
	}

	s.mu.RUnlock()
	atomic.AddInt64(&c.stats.getCount, 1)

	return value, ok
}

// GetStats returns cache statistics
func (c *Cache) GetStats() map[string]interface{} {
	var totalItems, hits, misses int64

	for _, s := range c.shards {
		s.mu.RLock()
		totalItems += int64(len(s.data))
		hits += atomic.LoadInt64(&s.hits)
		misses += atomic.LoadInt64(&s.misses)
		s.mu.RUnlock()
	}

	putCount := atomic.LoadInt64(&c.stats.putCount)
	getCount := atomic.LoadInt64(&c.stats.getCount)
	uptime := time.Since(c.stats.startTime).Seconds()

	var m runtime.MemStats
	runtime.ReadMemStats(&m)

	hitRate := 0.0
	if hits+misses > 0 {
		hitRate = float64(hits) * 100.0 / float64(hits+misses)
	}

	return map[string]interface{}{
		"total_items":          totalItems,
		"hit_rate":             hitRate,
		"memory_usage_percent": float64(m.Alloc) * 100.0 / float64(m.Sys),
		"put_count":            putCount,
		"get_count":            getCount,
		"hit_count":            hits,
		"miss_count":           misses,
		"uptime_seconds":       uptime,
		"requests_per_second":  float64(putCount+getCount) / uptime,
		"allocated_memory_mb":  float64(m.Alloc) / 1024 / 1024,
		"system_memory_mb":     float64(m.Sys) / 1024 / 1024,
		"evicted_count":        0, // No eviction implemented yet
	}
}

func main() {
	cache := NewCache()
	// logger := NewLogger()

	// HTTP handler for /put endpoint
	http.HandleFunc("/put", func(w http.ResponseWriter, r *http.Request) {
		// start := time.Now()

		if r.Method != http.MethodPost {
			http.Error(w, "Only POST method allowed", http.StatusMethodNotAllowed)
			return
		}

		var req struct {
			Key   string `json:"key"`
			Value string `json:"value"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			http.Error(w, "Invalid JSON", http.StatusBadRequest)
			return
		}

		cache.Put(req.Key, req.Value)
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]string{"status": "OK"})
	})

	http.HandleFunc("/get", func(w http.ResponseWriter, r *http.Request) {
		key := r.URL.Query().Get("key")
		if value, found := cache.Get(key); found {
			json.NewEncoder(w).Encode(map[string]string{"status": "OK", "key": key, "value": value})
		} else {
			json.NewEncoder(w).Encode(map[string]string{"status": "ERROR", "message": "Key not found."})
		}
	})

	fmt.Println("Server listening on port 7171...")
	log.Fatal(http.ListenAndServe(":7171", nil))
}

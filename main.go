package main

import (
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"sync"
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
	mu   sync.RWMutex
	data map[string]string
}

// Cache is a sharded in-memory key-value store.
type Cache struct {
	shards []*shard
}

// Logger handles non-blocking logging using a buffered channel.
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
}

// NewCache initializes the cache with a fixed number of shards.
func NewCache() *Cache {
	shards := make([]*shard, shardCount)
	for i := 0; i < shardCount; i++ {
		shards[i] = &shard{
			data: make(map[string]string),
		}
	}
	return &Cache{
		shards: shards,
	}
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
}

// Get retrieves the value for a given key.
func (c *Cache) Get(key string) (string, bool) {
	s := c.getShard(key)
	s.mu.RLock()
	value, ok := s.data[key]
	s.mu.RUnlock()
	return value, ok
}

func main() {
	cache := NewCache()
	logger := NewLogger()

	// HTTP handler for /put endpoint
	http.HandleFunc("/put", func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()

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

		// Enforce maximum length.
		if len(req.Key) > maxLength || len(req.Value) > maxLength {
			http.Error(w, "Key or value exceeds maximum length", http.StatusBadRequest)
			return
		}

		cache.Put(req.Key, req.Value)
		resp := map[string]string{
			"status":  "OK",
			"message": "Key inserted/updated successfully.",
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(resp)

		duration := time.Since(start)
		logger.Log("PUT /put key=%s value=%s took %s", req.Key, req.Value, duration)
	})

	// HTTP handler for /get endpoint
	http.HandleFunc("/get", func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()

		if r.Method != http.MethodGet {
			http.Error(w, "Only GET method allowed", http.StatusMethodNotAllowed)
			return
		}

		key := r.URL.Query().Get("key")
		if key == "" {
			http.Error(w, "Missing key parameter", http.StatusBadRequest)
			return
		}

		if value, found := cache.Get(key); found {
			resp := map[string]string{
				"status": "OK",
				"key":    key,
				"value":  value,
			}
			w.Header().Set("Content-Type", "application/json")
			json.NewEncoder(w).Encode(resp)
			logger.Log("GET /get key=%s found=true took %s", key, time.Since(start))
		} else {
			resp := map[string]string{
				"status":  "ERROR",
				"message": "Key not found.",
			}
			w.Header().Set("Content-Type", "application/json")
			json.NewEncoder(w).Encode(resp)
			logger.Log("GET /get key=%s found=false took %s", key, time.Since(start))
		}
	})

	fmt.Println("Server listening on port 7171...")
	log.Fatal(http.ListenAndServe(":7171", nil))
}

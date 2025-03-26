package main

import (
	"encoding/json"
	"fmt"
	"log"
	"runtime"
	"sync"
	"time"

	"github.com/cespare/xxhash/v2"
	"github.com/valyala/fasthttp"
)

const (
	maxLength    = 256
	shardCount   = 64
	ttlSeconds   = 10              // TTL for cache entries
	cleanupEvery = 5 * time.Second // Cleanup interval
)

type entry struct {
	value     string
	expiresAt int64 // Unix timestamp
}

type shard struct {
	mu   sync.RWMutex
	data map[string]entry
}

type Cache struct {
	shards []*shard
}

func NewCache() *Cache {
	shards := make([]*shard, shardCount)
	for i := 0; i < shardCount; i++ {
		shards[i] = &shard{
			data: make(map[string]entry),
		}
		go shards[i].startEviction()
	}
	return &Cache{shards: shards}
}

func (s *shard) startEviction() {
	ticker := time.NewTicker(cleanupEvery)
	defer ticker.Stop()
	for range ticker.C {
		s.mu.Lock()
		now := time.Now().Unix()
		for k, v := range s.data {
			if v.expiresAt <= now {
				delete(s.data, k)
			}
		}
		s.mu.Unlock()
	}
}

func (c *Cache) getShard(key string) *shard {
	hashVal := xxhash.Sum64String(key)
	index := int(hashVal & (shardCount - 1))
	return c.shards[index]
}

func (c *Cache) Put(key, value string) {
	if len(key) > maxLength || len(value) > maxLength {
		return
	}
	s := c.getShard(key)
	s.mu.Lock()
	s.data[key] = entry{
		value:     value,
		expiresAt: time.Now().Add(ttlSeconds * time.Second).Unix(),
	}
	s.mu.Unlock()
}

func (c *Cache) Get(key string) (string, bool) {
	s := c.getShard(key)
	s.mu.RLock()
	entry, ok := s.data[key]
	s.mu.RUnlock()
	if !ok || entry.expiresAt <= time.Now().Unix() {
		return "", false
	}
	return entry.value, true
}

func main() {
	runtime.GOMAXPROCS(2)
	cache := NewCache()

	requestHandler := func(ctx *fasthttp.RequestCtx) {
		path := string(ctx.Path())
		switch path {
		case "/put":
			if !ctx.IsPost() {
				ctx.Error("Only POST method allowed", fasthttp.StatusMethodNotAllowed)
				return
			}
			var req struct {
				Key   string `json:"key"`
				Value string `json:"value"`
			}
			if err := json.Unmarshal(ctx.PostBody(), &req); err != nil {
				ctx.Error("Invalid JSON", fasthttp.StatusBadRequest)
				return
			}
			cache.Put(req.Key, req.Value)
			ctx.Response.Header.Set("Content-Type", "application/json")
			json.NewEncoder(ctx.Response.BodyWriter()).Encode(map[string]string{"status": "OK"})

		case "/get":
			key := string(ctx.QueryArgs().Peek("key"))
			if value, found := cache.Get(key); found {
				ctx.Response.Header.Set("Content-Type", "application/json")
				json.NewEncoder(ctx.Response.BodyWriter()).Encode(map[string]string{"status": "OK", "key": key, "value": value})
			} else {
				ctx.Response.Header.Set("Content-Type", "application/json")
				json.NewEncoder(ctx.Response.BodyWriter()).Encode(map[string]string{"status": "ERROR", "message": "Key not found."})
			}

		default:
			ctx.Error("Endpoint not found", fasthttp.StatusNotFound)
		}
	}

	fmt.Println("Server listening on port 7171...")
	log.Fatal(fasthttp.ListenAndServe(":7171", requestHandler))
}

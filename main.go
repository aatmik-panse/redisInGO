package main

import (
	"encoding/json"
	"fmt"
	"hash/fnv"
	"log"
	"net/http"
	"sort"
	"sync"
)

const (
	maxLength       = 256
	shardCount      = 16  // Fixed number of physical shards
	virtualReplicas = 100 // Number of virtual nodes per shard for consistent hashing
)

// shard represents a single partition of the cache.
type shard struct {
	mu   sync.RWMutex
	data map[string]string
}

// Cache holds the physical shards and a consistent hash ring.
type Cache struct {
	shards []*shard
	hash   *ConsistentHash
}

// NewCache creates a sharded cache and initializes the consistent hash ring.
func NewCache() *Cache {
	shards := make([]*shard, shardCount)
	for i := 0; i < shardCount; i++ {
		shards[i] = &shard{
			data: make(map[string]string),
		}
	}
	chash := NewConsistentHash(shards, virtualReplicas)
	return &Cache{
		shards: shards,
		hash:   chash,
	}
}

// getShard returns the appropriate shard for a given key using consistent hashing.
func (c *Cache) getShard(key string) *shard {
	return c.hash.GetShard(key)
}

// Put inserts or updates the key-value pair in the appropriate shard.
func (c *Cache) Put(key, value string) {
	s := c.getShard(key)
	s.mu.Lock()
	defer s.mu.Unlock()
	s.data[key] = value
}

// Get retrieves the value for a given key from the appropriate shard.
func (c *Cache) Get(key string) (string, bool) {
	s := c.getShard(key)
	s.mu.RLock()
	defer s.mu.RUnlock()
	val, ok := s.data[key]
	return val, ok
}

// ConsistentHash implements a hash ring with virtual nodes.
type ConsistentHash struct {
	ring     []uint32          // Sorted hash ring
	nodes    map[uint32]*shard // Mapping from virtual node hash to physical shard
	replicas int               // Number of virtual nodes per physical shard
}

// NewConsistentHash initializes the hash ring with the given shards and number of replicas.
func NewConsistentHash(shards []*shard, replicas int) *ConsistentHash {
	ch := &ConsistentHash{
		ring:     []uint32{},
		nodes:    make(map[uint32]*shard),
		replicas: replicas,
	}
	// For each physical shard, add virtual nodes to the ring.
	for i, s := range shards {
		for j := 0; j < replicas; j++ {
			virtualNodeKey := fmt.Sprintf("shard-%d-replica-%d", i, j)
			hashVal := hash(virtualNodeKey)
			ch.ring = append(ch.ring, hashVal)
			ch.nodes[hashVal] = s
		}
	}
	// Sort the ring for efficient lookup.
	sort.Slice(ch.ring, func(i, j int) bool {
		return ch.ring[i] < ch.ring[j]
	})
	return ch
}

// GetShard returns the shard responsible for the provided key using binary search.
func (ch *ConsistentHash) GetShard(key string) *shard {
	if len(ch.ring) == 0 {
		return nil
	}
	h := hash(key)
	// Binary search for appropriate virtual node.
	idx := sort.Search(len(ch.ring), func(i int) bool { return ch.ring[i] >= h })
	// Wrap around the ring if needed.
	if idx == len(ch.ring) {
		idx = 0
	}
	return ch.nodes[ch.ring[idx]]
}

// hash computes a 32-bit FNV-1a hash for the given string.
func hash(s string) uint32 {
	h := fnv.New32a()
	h.Write([]byte(s))
	return h.Sum32()
}

func main() {
	cache := NewCache()

	http.HandleFunc("/put", func(w http.ResponseWriter, r *http.Request) {
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

		// Enforce maximum length constraints.
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
	})

	http.HandleFunc("/get", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			http.Error(w, "Only GET method allowed", http.StatusMethodNotAllowed)
			return
		}

		key := r.URL.Query().Get("key")
		if key == "" {
			http.Error(w, "Missing key parameter", http.StatusBadRequest)
			return
		}

		if val, found := cache.Get(key); found {
			resp := map[string]string{
				"status": "OK",
				"key":    key,
				"value":  val,
			}
			w.Header().Set("Content-Type", "application/json")
			json.NewEncoder(w).Encode(resp)
		} else {
			resp := map[string]string{
				"status":  "ERROR",
				"message": "Key not found.",
			}
			w.Header().Set("Content-Type", "application/json")
			json.NewEncoder(w).Encode(resp)
		}
	})

	fmt.Println("Server listening on port 7171...")
	log.Fatal(http.ListenAndServe(":7171", nil))
}

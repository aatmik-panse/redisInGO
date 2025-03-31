package main

import (
	"flag"
	"fmt"
	"log"
	"os"
	"time"

	"goredis/sdk"
)

func main() {
	// Command line flags
	hostname := flag.String("host", "localhost", "Cache server hostname")
	port := flag.Int("port", sdk.DefaultPort, "Cache server port")
	poolSize := flag.Int("pool", 5, "Connection pool size")
	timeout := flag.Duration("timeout", sdk.DefaultTimeout, "Operation timeout")

	flag.Parse()

	// Configure client options
	options := &sdk.ClientOptions{
		Port:           *port,
		PoolSize:       *poolSize,
		ConnectTimeout: *timeout,
		ReadTimeout:    *timeout,
		WriteTimeout:   *timeout,
	}

	// Create client
	client := sdk.NewClient(*hostname, options)
	defer client.Close()

	fmt.Printf("Connected to cache server at %s\n", client.GetServerAddress())

	// Example operations
	key := "example-key"
	value := "example-value-" + time.Now().Format(time.RFC3339)

	// PUT operation
	fmt.Printf("Storing key: %s, value: %s\n", key, value)
	err := client.Put(key, value)
	if err != nil {
		log.Printf("Failed to store key: %v", err)
		os.Exit(1)
	}
	fmt.Println("Successfully stored key-value pair")

	// GET operation
	fmt.Printf("Retrieving value for key: %s\n", key)
	retrievedValue, err := client.Get(key)
	if err != nil {
		log.Printf("Failed to retrieve key: %v", err)
		os.Exit(1)
	}

	fmt.Printf("Retrieved value: %s\n", retrievedValue)

	// Verify values match
	if retrievedValue == value {
		fmt.Println("✅ Values match!")
	} else {
		fmt.Printf("❌ Values don't match! Expected: %s, Got: %s\n", value, retrievedValue)
	}

	// Try to get a non-existent key
	nonExistentKey := "non-existent-key-" + time.Now().Format(time.RFC3339)
	fmt.Printf("Trying to retrieve non-existent key: %s\n", nonExistentKey)

	_, err = client.Get(nonExistentKey)
	if err == sdk.ErrKeyNotFound {
		fmt.Println("✅ Correctly got 'key not found' error")
	} else if err != nil {
		fmt.Printf("❌ Got unexpected error: %v\n", err)
	} else {
		fmt.Println("❌ Expected 'key not found' error but got a value")
	}

	// Multi-operation performance test
	testCount := 100
	fmt.Printf("\nPerforming %d PUT operations...\n", testCount)
	startTime := time.Now()
	for i := 0; i < testCount; i++ {
		testKey := fmt.Sprintf("test-key-%d", i)
		testValue := fmt.Sprintf("test-value-%d", i)

		if err := client.Put(testKey, testValue); err != nil {
			log.Printf("Failed to store key %s: %v", testKey, err)
		}
	}

	putDuration := time.Since(startTime)
	fmt.Printf("PUT operations completed in %v (%v per operation)\n",
		putDuration, putDuration/time.Duration(testCount))

	fmt.Printf("\nPerforming %d GET operations...\n", testCount)
	startTime = time.Now()
	for i := 0; i < testCount; i++ {
		testKey := fmt.Sprintf("test-key-%d", i)

		_, err := client.Get(testKey)
		if err != nil && err != sdk.ErrKeyNotFound {
			log.Printf("Failed to retrieve key %s: %v", testKey, err)
		}
	}

	getDuration := time.Since(startTime)
	fmt.Printf("GET operations completed in %v (%v per operation)\n",
		getDuration, getDuration/time.Duration(testCount))

	fmt.Println("\nSDK test completed successfully")
}

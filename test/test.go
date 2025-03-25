package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"sync"
	"time"
)

const (
	serverURL   = "http://localhost:7171"
	initialLoad = 100   // Starting number of requests
	maxLoad     = 50000 // Maximum number of requests
	stepSize    = 100   // Step increment per iteration
	concurrency = 100   // Number of concurrent workers
)

type Payload struct {
	Key   string `json:"key"`
	Value string `json:"value"`
}

func putRequest(key, value string, wg *sync.WaitGroup, success *int32) {
	defer wg.Done()
	payload, _ := json.Marshal(Payload{Key: key, Value: value})
	resp, err := http.Post(serverURL+"/put", "application/json", bytes.NewBuffer(payload))
	if err == nil && resp.StatusCode == http.StatusOK {
		*success++
	}
}

func getRequest(key string, wg *sync.WaitGroup, success *int32) {
	defer wg.Done()
	resp, err := http.Get(fmt.Sprintf("%s/get?key=%s", serverURL, key))
	if err == nil && resp.StatusCode == http.StatusOK {
		*success++
	}
}

func main() {
	var bestPutRate, bestGetRate float64
	var bestPutLoad, bestGetLoad int

	fmt.Println("Starting load test with increasing load...")

	for load := initialLoad; load <= maxLoad; load += stepSize {
		var wg sync.WaitGroup
		var putSuccess, getSuccess int32

		fmt.Printf("Testing with %d requests...\n", load)
		tStart := time.Now()
		// PUT requests
		for i := 0; i < load; i++ {
			wg.Add(1)
			go putRequest(fmt.Sprintf("key%d", i), fmt.Sprintf("value%d", i), &wg, &putSuccess)
			if i%concurrency == 0 {
				wg.Wait()
			}
		}
		wg.Wait()
		putDuration := time.Since(tStart).Seconds()
		putRate := float64(load) / putDuration
		if putRate > bestPutRate {
			bestPutRate = putRate
			bestPutLoad = load
		}

		tStart = time.Now()
		// GET requests
		for i := 0; i < load; i++ {
			wg.Add(1)
			go getRequest(fmt.Sprintf("key%d", i), &wg, &getSuccess)
			if i%concurrency == 0 {
				wg.Wait()
			}
		}
		wg.Wait()
		getDuration := time.Since(tStart).Seconds()
		getRate := float64(load) / getDuration
		if getRate > bestGetRate {
			bestGetRate = getRate
			bestGetLoad = load
		}
	}

	fmt.Printf("Best PUT performance: %.2f requests/sec at %d requests\n", bestPutRate, bestPutLoad)
	fmt.Printf("Best GET performance: %.2f requests/sec at %d requests\n", bestGetRate, bestGetLoad)
}

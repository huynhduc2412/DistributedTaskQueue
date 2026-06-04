package main

import (
	"bytes"
	"log"
	"net/http"
	"sync"
	"sync/atomic"
	"time"
)

func main() {
	totalRequests := 20 // nums request
	concurrency := 1    // nums goroutine run concurrency

	url := "http://localhost:8080/submit"
	payload := []byte(`{"type":"EMAIL","to":"stress@test.com"}`)

	client := &http.Client{
		Timeout: 5 * time.Second,
	}

	var wg sync.WaitGroup
	ch := make(chan struct{}, concurrency)

	var successCount int64
	var failCount int64

	startTime := time.Now()
	log.Printf("Starting stress test: %d requests, concurrency=%d",
		totalRequests, concurrency)

	for i := 0; i < totalRequests; i++ {
		wg.Add(1)
		ch <- struct{}{}

		go func() {
			defer wg.Done()
			defer func() { <-ch }()

			resp, err := client.Post(
				url,
				"application/json",
				bytes.NewBuffer(payload),
			)

			if err != nil {
				atomic.AddInt64(&failCount, 1)
				return
			}
			defer resp.Body.Close()

			if resp.StatusCode >= 200 && resp.StatusCode < 300 {
				atomic.AddInt64(&successCount, 1)
			} else {
				atomic.AddInt64(&failCount, 1)
			}
		}()
	}

	wg.Wait()

	duration := time.Since(startTime)

	log.Println("========== RESULT ==========")
	log.Printf("Total Requests : %d", totalRequests)
	log.Printf("Success        : %d", successCount)
	log.Printf("Failed         : %d", failCount)
	log.Printf("Duration       : %v", duration)
	log.Printf("Requests/sec   : %.2f",
		float64(totalRequests)/duration.Seconds())
}
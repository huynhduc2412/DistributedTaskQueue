package main

import (
	"bytes"
	"flag"
	"io"
	"log"
	"net/http"
	"sort"
	"sync"
	"sync/atomic"
	"time"
)

func main() {
	totalRequests := flag.Int("requests", 100, "number of requests to send")
	concurrency := flag.Int("concurrency", 20, "maximum concurrent requests")
	url := flag.String("url", "http://localhost:8080/submit", "producer submit endpoint")
	steadyDuration := flag.Duration("duration", 0, "optional steady-load test duration, for example 1m")
	rate := flag.Int("rate", 0, "requests per second when -duration is set")
	flag.Parse()

	if *totalRequests <= 0 || *concurrency <= 0 {
		log.Fatal("requests and concurrency must be greater than zero")
	}
	if *steadyDuration < 0 || (*steadyDuration > 0 && *rate <= 0) {
		log.Fatal("duration must not be negative and rate must be greater than zero when duration is set")
	}

	payload := []byte(`{"type":"EMAIL","to":"stress@test.com"}`)

	client := &http.Client{
		Timeout: 5 * time.Second,
	}

	var wg sync.WaitGroup

	ch := make(chan struct{}, *concurrency)

	var successCount int64
	var failCount int64
	var sentCount int64
	latencies := make([]time.Duration, 0, *totalRequests)
	var latenciesMu sync.Mutex

	startTime := time.Now()
	if *steadyDuration > 0 {
		log.Printf("Starting steady-load test: duration=%s, rate=%d requests/sec, concurrency=%d", *steadyDuration, *rate, *concurrency)
	} else {
		log.Printf("Starting stress test: %d requests, concurrency=%d", *totalRequests, *concurrency)
	}

	sendRequest := func() {
		wg.Add(1)
		ch <- struct{}{}
		atomic.AddInt64(&sentCount, 1)

		go func() {
			defer wg.Done()
			defer func() { <-ch }()

			requestStart := time.Now()
			resp, err := client.Post(
				*url,
				"application/json",
				bytes.NewBuffer(payload),
			)
			latency := time.Since(requestStart)
			latenciesMu.Lock()
			latencies = append(latencies, latency)
			latenciesMu.Unlock()

			if err != nil {
				atomic.AddInt64(&failCount, 1)
				return
			}
			defer resp.Body.Close()
			_, _ = io.Copy(io.Discard, resp.Body)

			if resp.StatusCode >= 200 && resp.StatusCode < 300 {
				atomic.AddInt64(&successCount, 1)
			} else {
				atomic.AddInt64(&failCount, 1)
			}
		}()
	}

	if *steadyDuration > 0 {
		interval := time.Second / time.Duration(*rate)
		ticker := time.NewTicker(interval)
		deadline := time.NewTimer(*steadyDuration)

	loadLoop:
		for {
			select {
			case <-ticker.C:
				sendRequest()
			case <-deadline.C:
				break loadLoop
			}
		}
		ticker.Stop()
	} else {
		for i := 0; i < *totalRequests; i++ {
			sendRequest()
		}
	}

	wg.Wait()

	elapsed := time.Since(startTime)
	totalSent := atomic.LoadInt64(&sentCount)

	log.Println("========== RESULT ==========")
	log.Printf("Total Requests : %d", totalSent)
	log.Printf("Success        : %d", successCount)
	log.Printf("Failed         : %d", failCount)
	log.Printf("Duration       : %v", elapsed)
	log.Printf("Requests/sec   : %.2f",
		float64(totalSent)/elapsed.Seconds())
	log.Printf("Failure rate   : %.2f%%", float64(failCount)*100/float64(totalSent))
	log.Printf("Latency p50    : %v", percentile(latencies, 0.50))
	log.Printf("Latency p95    : %v", percentile(latencies, 0.95))
	log.Printf("Latency p99    : %v", percentile(latencies, 0.99))
}

// percentile returns the nearest-rank percentile from a completed benchmark run.
func percentile(values []time.Duration, p float64) time.Duration {
	if len(values) == 0 {
		return 0
	}

	sort.Slice(values, func(i, j int) bool { return values[i] < values[j] })
	index := int(float64(len(values))*p+0.999999) - 1
	if index < 0 {
		index = 0
	}
	if index >= len(values) {
		index = len(values) - 1
	}
	return values[index]
}

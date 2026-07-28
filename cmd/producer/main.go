package main

import (
	"encoding/json"
	"log"
	"net/http"

	"github.com/google/uuid"
	"github.com/huynhduc2412/DistributedTaskQueue/internal/broker"
	"github.com/huynhduc2412/DistributedTaskQueue/internal/config"
	"github.com/prometheus/client_golang/prometheus/promhttp"
)

const maxLimit = 50000

func main() {
	cfg := config.Load()
	rb := broker.NewRedisBroker(cfg.RedisAdrr, 10)

	mux := http.NewServeMux()
	mux.Handle("/metrics", promhttp.Handler())
	mux.Handle("/submit", instrumentProducer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		defer r.Body.Close()
		if r.Method != http.MethodPost {
			http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
			return
		}

		//check rate limit for queue
		queueLength, err := rb.Client.XLen(r.Context(), cfg.StreamName).Result()
		if err == nil && queueLength > maxLimit {
			admissionRejected.Inc()
			w.WriteHeader(http.StatusTooManyRequests)
			w.Write([]byte("ovarload system , please try again in a few minutes"))
			return
		}

		var data map[string]interface{}
		if err := json.NewDecoder(r.Body).Decode(&data); err != nil {
			http.Error(w, "Invalid JSON", http.StatusBadRequest)
			return
		}

		if data == nil {
			data = make(map[string]interface{})
		}

		data["job_id"] = uuid.New().String()

		if err := rb.Publish(r.Context(), cfg.StreamName, data); err != nil {
			enqueueFailures.Inc()
			http.Error(w, "Failed to publish job", http.StatusInternalServerError)
			return
		}

		tasksEnqueued.Inc()
		w.WriteHeader(http.StatusOK)
		w.Write([]byte("Job Enqueued"))
	})))

	log.Println("Producer API and metrics are listening on :8080")
	if err := http.ListenAndServe(":8080", mux); err != nil {
		log.Fatalf("Producer server unavailable on port 8080: %v", err)
	}
}

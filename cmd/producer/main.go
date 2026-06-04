package main

import (
	"encoding/json"
	"net/http"

	"github.com/google/uuid"
	"github.com/huynhduc2412/DistributedTaskQueue/internal/broker"
	"github.com/huynhduc2412/DistributedTaskQueue/internal/config"
)
const maxLimit = 50000


func main() {
	cfg := config.Load()
	rb := broker.NewRedisBroker(cfg.RedisAdrr, 10)

	http.HandleFunc("/submit", func(w http.ResponseWriter, r *http.Request) {
		defer r.Body.Close()
		if r.Method != http.MethodPost {
			http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
			return
		}

		//check rate limit for queue
		queueLength , err := rb.Client.XLen(r.Context() , cfg.StreamName).Result()
		if err == nil && queueLength > maxLimit {
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
			http.Error(w, "Failed to publish job", http.StatusInternalServerError)
			return
		}

		w.WriteHeader(http.StatusOK)
		w.Write([]byte("Job Enqueued"))
	})
	http.ListenAndServe(":8080", nil)
}

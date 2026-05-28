package main

import (
	"encoding/json"
	"net/http"

	"github.com/google/uuid"
	"github.com/huynhduc2412/DistributedTaskQueue/internal/broker"
	"github.com/huynhduc2412/DistributedTaskQueue/internal/config"
)

func main() {
	cfg := config.Load()
	rb := broker.NewRedisBroker(cfg.RedisAdrr, 10)

	http.HandleFunc("/submit", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
			return
		}

		defer r.Body.Close()

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

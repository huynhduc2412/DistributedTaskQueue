package producer

import (
	"encoding/json"
	"net/http"

	"github.com/google/uuid"
	"github.com/huynhduc2412/DistributedTaskQueue/internal/broker"
	"github.com/huynhduc2412/DistributedTaskQueue/internal/config"
)

func main() {
	cfg := config.Load()
	rb := broker.NewRedisBroker(cfg.RedisAdrr , 10)

	http.HandleFunc("/submit" , func(w http.ResponseWriter, r *http.Request){
		var data map[string]interface{}
		json.NewDecoder(r.Body).Decode(&data)

		data["job_id"] = uuid.New().String()
		rb.Publish(r.Context() , cfg.StreamName , data)
		w.Write([]byte("Job Enqueued"))
		
	})
	http.ListenAndServe(":8080" , nil)
}
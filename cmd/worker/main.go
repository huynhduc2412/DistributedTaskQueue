package main

import (
	"context"
	"log"
	"net/http"
	"os"
	"os/signal"
	"runtime"
	"syscall"
	"time"

	"github.com/huynhduc2412/DistributedTaskQueue/internal/broker"
	"github.com/huynhduc2412/DistributedTaskQueue/internal/config"
	"github.com/huynhduc2412/DistributedTaskQueue/internal/task"
	"github.com/huynhduc2412/DistributedTaskQueue/internal/workerpool"
	"github.com/prometheus/client_golang/prometheus/promhttp"
)
func main() {
	cfg := config.Load()
	//init broker (redis)
	rb := broker.NewRedisBroker(cfg.RedisAdrr , runtime.GOMAXPROCS(0) + 5)

	//registry handler (business logic)
	task.Register("EMAIL" , task.WithIdempotency(rb.Client , task.HandleEmail))
	task.Register("IMAGE" , task.WithIdempotency(rb.Client , task.HandleImage))

	podName := os.Getenv("POD_NAME")
	if podName == ""{
		podName = "local-worker"
	}
	pool := workerpool.NewPool(cfg , rb , podName)

	go func ()  {
		http.Handle("/metrics" , promhttp.Handler())
		log.Println("Worker metrics is openning at port : 8082/metrics")
		if err := http.ListenAndServe(":8082" , nil) ; err != nil {
			log.Printf("Error metric worker at port 8082: %v" , err)
		}	
	}()

	go func ()  {
		ticker := time.NewTicker(30 * time.Second)
		for range ticker.C {
			s := pool.GetStatus()
			log.Printf("STAS WorkerPool: Success: %d | Failed: %d | Uptime: %s" , s.SuccessCount , s.FailureCount , s.Uptime)
		}	
	}()

	ctx , stop := signal.NotifyContext(context.Background() , os.Interrupt , syscall.SIGTERM)
	
	err := rb.Client.XGroupCreateMkStream(ctx , cfg.StreamName , cfg.GroupName , "0").Err()
	if err != nil {
		if err.Error() != "BUSYGROUP Consumer Group name already exists" {
			log.Fatalf("Can't created Group name: %v" , err)
		}
	}else{
		log.Printf("Created successfully Consumer Group [%s] for Stream [%s]" , cfg.GroupName , cfg.StreamName)
	}

	defer stop()
	pool.Run(ctx)
}
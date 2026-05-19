package worker

import (
	"context"
	"os"
	"os/signal"
	"runtime"
	"syscall"

	"github.com/huynhduc2412/DistributedTaskQueue/internal/broker"
	"github.com/huynhduc2412/DistributedTaskQueue/internal/config"
	"github.com/huynhduc2412/DistributedTaskQueue/internal/task"
	"github.com/huynhduc2412/DistributedTaskQueue/internal/workerpool"
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

	ctx , stop := signal.NotifyContext(context.Background() , os.Interrupt , syscall.SIGTERM)

	defer stop()
	pool.Run(ctx)
}
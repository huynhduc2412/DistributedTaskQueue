package workerpool

import (
	"context"
	"log"
	"runtime"
	"sync"
	"time"

	"github.com/huynhduc2412/DistributedTaskQueue/internal/broker"
	"github.com/huynhduc2412/DistributedTaskQueue/internal/config"
	"github.com/huynhduc2412/DistributedTaskQueue/internal/task"
	"github.com/redis/go-redis/v9"
)

type WorkerPool struct {
	cfg *config.Config
	broker *broker.RedisBroker
	taskChan chan redis.XMessage
	numWorkers int
	wg sync.WaitGroup
	podName string
}

func NewPool(cfs *config.Config , rb *broker.RedisBroker , podName string) *WorkerPool {
	cntWorkers := runtime.GOMAXPROCS(0)
	return &WorkerPool{
		cfg: cfs,
		broker: rb,
		numWorkers: cntWorkers,
		taskChan: make(chan redis.XMessage , cntWorkers * 2),
		podName: podName,
	}
}

func(p *WorkerPool) Run(ctx context.Context) {
	log.Printf("Worker pool [%s] is starting with %d workers\n" , p.podName , p.numWorkers)

	//active worker
	for i := 1 ; i <= p.numWorkers ; i++ {
		p.wg.Add(1)
	}

	p.wg.Add(1)
	p.dispatcher(ctx)
	p.wg.Wait()
	log.Println("workers done tasks!!")
}
//read data and send data to workersPool 
func(p *WorkerPool) dispatcher(ctx context.Context) {
	defer p.wg.Done()
	for {
		select{
		case <-ctx.Done():
			log.Println(ctx.Err())
			close(p.taskChan)
		default:
			msgs , err := p.broker.ReadBatch(ctx , p.cfg.StreamName ,  p.cfg.GroupName , p.podName , 5)
			if err != nil {
				time.Sleep(1 * time.Second)
				continue
			}
			for _ , msg := range msgs{
				p.taskChan <- msg
			}
		}
	}
}

func(p *WorkerPool) worker(ctx context.Context , id int) {
	defer p.wg.Done()
	for msg := range p.taskChan {
		p.exucteTask(ctx , msg)
	}
}

func(p *WorkerPool) exucteTask(ctx context.Context , msg redis.XMessage) {
	taskType := msg.Values["type"].(string)
	// get handler in global registry
	handler , err := task.GetHandler(taskType)
	
	if err != nil {
		log.Printf("Not found registry for %s" , taskType)
		p.broker.Ack(ctx , p.cfg.StreamName , p.cfg.GroupName , msg.ID)
		return
	}

	if err = handler(ctx , msg.Values); err != nil {
		log.Printf("Error Task: %v and Move on DLQ." , err)
		p.broker.Publish(ctx , p.cfg.DLQStream , msg.Values)
	}
	p.broker.Ack(ctx , p.cfg.StreamName , p.cfg.GroupName , msg.ID)
}

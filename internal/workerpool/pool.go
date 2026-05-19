package workerpool

import (
	"context"
	"log"
	"runtime"
	"strconv"
	"sync"
	"sync/atomic"
	"time"

	"github.com/huynhduc2412/DistributedTaskQueue/internal/broker"
	"github.com/huynhduc2412/DistributedTaskQueue/internal/config"
	"github.com/huynhduc2412/DistributedTaskQueue/internal/task"
	"github.com/redis/go-redis/v9"
)

const maxRetries = 3

type WorkerStatus struct {
	SuccessCount uint64 `json:"success_count"`
	FailureCount uint64 `json:"failure_count"`
	RetryCount uint64 `json:"retry_count"`
	Uptime string `json:"up_time"`
	Workers int `json:"workers"`
	PodName string `json:"pod_name"`
}

type WorkerPool struct {
	cfg *config.Config
	broker *broker.RedisBroker
	taskChan chan redis.XMessage
	numWorkers int
	wg sync.WaitGroup
	podName string
	successCount uint64
	failureCount uint64
	retryCount uint64
	startTime time.Time
}

func NewPool(cfs *config.Config , rb *broker.RedisBroker , podName string) *WorkerPool {
	cntWorkers := runtime.GOMAXPROCS(0)
	return &WorkerPool{
		cfg: cfs,
		broker: rb,
		numWorkers: cntWorkers,
		taskChan: make(chan redis.XMessage , cntWorkers * 2),
		podName: podName,
		startTime: time.Now(),
	}
}

func(p *WorkerPool) GetStatus() WorkerStatus {
	return WorkerStatus{
		SuccessCount: atomic.LoadUint64(&p.successCount),
		FailureCount: atomic.LoadUint64(&p.failureCount),
		RetryCount: atomic.LoadUint64(&p.retryCount),
		Uptime: time.Since(p.startTime).String(),
		Workers: p.numWorkers,
		PodName: p.podName,
	}
}

func(p *WorkerPool) Run(ctx context.Context) {
	log.Printf("Worker pool [%s] is starting with %d workers\n" , p.podName , p.numWorkers)

	//active worker
	for i := 1 ; i <= p.numWorkers ; i++ {
		p.wg.Add(1)
		go p.worker(ctx , i)
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
		p.hanldRetry(ctx , msg , err)
	}else{
		atomic.AddUint64(&p.successCount , 1)
		p.broker.Ack(ctx , p.cfg.StreamName , p.cfg.GroupName , msg.ID)
	}
}

func (p *WorkerPool) hanldRetry(ctx context.Context , msg redis.XMessage , err error) {
	// log.Printf("Error Task: %v and try retry if not ,move on DLQ." , err)
	retryCount := 0
	if val , ok := msg.Values["retry_count"].(string) ; ok {
		retryCount , _ = strconv.Atoi(val)
	}
	if retryCount <= maxRetries {
		atomic.AddUint64(&p.retryCount , 1)
		retryCount++

		msg.Values["retry_count"] = strconv.Itoa(retryCount)
		msg.Values["last_error"] = err.Error()
		
		err = p.broker.Publish(ctx , p.cfg.StreamName , msg.Values)
		if err != nil {
			log.Printf("Error Pushed task back on main stream Queue: %v" , err)
		}else{
			log.Printf("retry %d times for Task %s" , retryCount , msg.ID)
		}
	}else {
		//move on DLQ
		p.moveToDLQ(ctx , msg , err)
	}
	//ack old task to remove on PEL
	p.broker.Ack(ctx , p.cfg.StreamName , p.cfg.GroupName , msg.ID)
}

func(p *WorkerPool) moveToDLQ(ctx context.Context , msg redis.XMessage , finalErr error){
	atomic.AddUint64(&p.failureCount , 1)
	
	msg.Values["final_error"] = finalErr.Error()
	msg.Values["failed_at"] = time.Now().Format(time.RFC3339)
	
	err := p.broker.Publish(ctx , p.cfg.DLQStream , msg.Values)
	if err != nil {
		log.Printf("Error when pushed on DLQ: %v" , err)
	}else{
		log.Printf("Success move on DLQ with taskId %s" , msg.ID)
	}
}

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
	"github.com/prometheus/client_golang/prometheus"
	"github.com/redis/go-redis/v9"
)

var (
	tasksProcessed = prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Name: "queue_tasks_processed_total",
			Help: "The number of tasks was done",
		},
		[]string{"status", "pod_name"},
	)
)

func init() {
	prometheus.MustRegister(tasksProcessed)
}

const maxRetries = 3

type WorkerStatus struct {
	SuccessCount uint64 `json:"success_count"`
	FailureCount uint64 `json:"failure_count"`
	RetryCount   uint64 `json:"retry_count"`
	Uptime       string `json:"up_time"`
	Workers      int    `json:"workers"`
	PodName      string `json:"pod_name"`
}

type WorkerPool struct {
	cfg          *config.Config
	broker       *broker.RedisBroker
	taskChan     chan redis.XMessage
	numWorkers   int
	wg           sync.WaitGroup
	podName      string
	successCount uint64
	failureCount uint64
	retryCount   uint64
	startTime    time.Time
}

func NewPool(cfs *config.Config, rb *broker.RedisBroker, podName string) *WorkerPool {
	cntWorkers := runtime.GOMAXPROCS(0)
	return &WorkerPool{
		cfg:        cfs,
		broker:     rb,
		numWorkers: cntWorkers,
		taskChan:   make(chan redis.XMessage, cntWorkers*2),
		podName:    podName,
		startTime:  time.Now(),
	}
}

func (p *WorkerPool) GetStatus() WorkerStatus {
	return WorkerStatus{
		SuccessCount: atomic.LoadUint64(&p.successCount),
		FailureCount: atomic.LoadUint64(&p.failureCount),
		RetryCount:   atomic.LoadUint64(&p.retryCount),
		Uptime:       time.Since(p.startTime).String(),
		Workers:      p.numWorkers,
		PodName:      p.podName,
	}
}

func (p *WorkerPool) Run(ctx context.Context) {
	log.Printf("Worker pool [%s] is starting with %d workers\n", p.podName, p.numWorkers)

	//active worker
	for i := 1; i <= p.numWorkers; i++ {
		p.wg.Add(1)
		go p.worker(ctx, i)
	}

	p.wg.Add(1)
	go p.dispatcher(ctx) //read new task

	// p.wg.Add(1)
	// go p.reclaimPendingTasks(ctx) // read again stuck task for reasons (crash worker , bug ,...)

	p.wg.Wait()
	log.Println("workers done tasks!!")
}

// PEL SCAN STREAM IS STUCK (Use XAutoClaim)
func (p *WorkerPool) reclaimPendingTasks(ctx context.Context) {
	ticker := time.NewTicker(5 * time.Second)
	defer p.wg.Done()
	defer ticker.Stop()
	//start scanning oldest tasks
	startId := "0-0"

	for {
		select {
		case <-ctx.Done():
			log.Println(ctx.Err())
			return
		case <-ticker.C:
			messages, nxtId, err := p.broker.Client.XAutoClaim(ctx, &redis.XAutoClaimArgs{
				Stream:   p.cfg.StreamName,
				Group:    p.cfg.GroupName,
				Consumer: p.podName,
				MinIdle:  30 * time.Second, //definitely worker crash or bug about 30s is considered to stuck
				Start:    startId,
				Count:    5,
			}).Result()

			if err != nil {
				continue
			}

			//update newId for next loop
			startId = nxtId

			for _, msg := range messages {
				select {
				case p.taskChan <- msg:
					log.Printf("Save taskId %s exit worker crash or bug", msg.ID)
				case <-ctx.Done():
					log.Println(ctx.Err())
					return
				}
			}
		}
	}
}

// read data and send data to workersPool
func (p *WorkerPool) dispatcher(ctx context.Context) {
	defer p.wg.Done()
	defer close(p.taskChan)
	for {
		select {
		case <-ctx.Done():
			log.Println(ctx.Err())
			return
		default:
			msgs, err := p.broker.ReadBatch(ctx, p.cfg.StreamName, p.cfg.GroupName, p.podName, int64(p.numWorkers))
			if err != nil {
				log.Println(err.Error())
				time.Sleep(1 * time.Second)
				continue
			}
			for _, msg := range msgs {
				select {
				case p.taskChan <- msg:
				case <-ctx.Done():
					//dont give task anymore
					log.Println(ctx.Err())
					return
				}
			}
		}
	}
}

func (p *WorkerPool) worker(ctx context.Context, id int) {
	defer p.wg.Done()
	for msg := range p.taskChan {
		p.exucteTask(ctx, msg, id)
	}
}

func (p *WorkerPool) exucteTask(ctx context.Context, msg redis.XMessage, workerId int) {
	//distributed lock and imdempotency task
	taskId := msg.ID
	lockKey := "lock:task" + taskId
	res, err := p.broker.Client.SetArgs(ctx, lockKey, "processing", redis.SetArgs{
		TTL:  30 * time.Second,
		Mode: "NX",
	}).Result()

	if err != nil {
		log.Printf("[Worker-%d] check imdepotency wrong for task %s: %v", workerId, taskId, err)
		return
	}
	//other worker exucuted on this task
	if res != "OK" {
		log.Printf("[Worker-%d] executing task was executed by other worker", workerId)
		p.broker.Ack(ctx, p.cfg.StreamName, p.cfg.GroupName, msg.ID)
		return
	}

	log.Printf("[Worker-%d] Key obtained successfully. Task processing begins: %s", workerId, taskId)

	taskType := msg.Values["type"].(string)
	// get handler in global registry
	handler, err := task.GetHandler(taskType)

	if err != nil {
		log.Printf("Not found registry for %s", taskType)
		p.broker.Ack(ctx, p.cfg.StreamName, p.cfg.GroupName, msg.ID)
		return
	}
	if err = handler(ctx, msg.Values); err != nil {
		p.hanldRetry(ctx, msg, err)
	} else {
		atomic.AddUint64(&p.successCount, 1)
		tasksProcessed.WithLabelValues("success", p.podName).Inc()
		p.broker.Ack(ctx, p.cfg.StreamName, p.cfg.GroupName, msg.ID)
	}
	_, err = p.broker.Client.Del(ctx, lockKey).Result()
	if err != nil {
		log.Printf("Deleted lockKey for task executed error : %v", err)
		return
	}
}

// buildLuaArgs package [GroupName, MsgID, key1, val1, key2, val2...]
func buildLuaArgs(groupName, msgID string, values map[string]interface{}) []interface{} {
	args := make([]interface{}, 0, 2+len(values)*2)
	args = append(args, groupName, msgID)

	for k, v := range values {
		args = append(args, k, v)
	}
	return args
}

var luaRetryScript = redis.NewScript(`
    -- KEYS[1]: Main Stream Name
    -- ARGV[1]: Consumer Group Name
    -- ARGV[2]: Old Message ID to ACK
    -- ARGV[3...]: Payload Key-Value Pairs

    -- 1. Push task into main streem again for retry
    local new_id = redis.call('XADD', KEYS[1], '*', unpack(ARGV, 3))

    -- 2. ACK remove old task from PEL of main stream
    redis.call('XACK', KEYS[1], ARGV[1], ARGV[2])

    return new_id
`)



func (p *WorkerPool) hanldRetry(ctx context.Context, msg redis.XMessage, err error) {
	retryCount := 0
	if val, ok := msg.Values["retry_count"].(string); ok {
		retryCount, _ = strconv.Atoi(val)
	}
	if retryCount <= maxRetries {
		atomic.AddUint64(&p.retryCount, 1)
		tasksProcessed.WithLabelValues("retry", p.podName).Inc()
		retryCount++

		msg.Values["retry_count"] = strconv.Itoa(retryCount)
		msg.Values["last_error"] = err.Error()

		args := buildLuaArgs(p.cfg.GroupName, msg.ID, msg.Values)

		_ , err := luaRetryScript.Run(ctx, p.broker.Client, []string{p.cfg.StreamName}, args...).Result()

		if err != nil {
			log.Printf("Error luaRetryScript: %v", err)
		} else {
			log.Printf("retry %d times for Task %s", retryCount, msg.ID)
		}
	} else {
		//move on DLQ
		p.moveToDLQ(ctx, msg, err)
		p.broker.Ack(ctx, p.cfg.StreamName, p.cfg.GroupName, msg.ID)
	}
}


var luaDLQScript = redis.NewScript(`
    -- KEYS[1]: Main Stream Name
    -- KEYS[2]: DLQ Stream Name
    -- ARGV[1]: Consumer Group Name
    -- ARGV[2]: Old Message ID to ACK
    -- ARGV[3...]: Payload Key-Value Pairs

    -- 1. Push failed task into DLQ stream
    local dlq_id = redis.call('XADD', KEYS[2], '*', unpack(ARGV, 3))

    -- 2. ACK old task from PEL of main stream
    redis.call('XACK', KEYS[1], ARGV[1], ARGV[2])

    return dlq_id
`)

func (p *WorkerPool) moveToDLQ(ctx context.Context, msg redis.XMessage, finalErr error) {
	atomic.AddUint64(&p.failureCount, 1)
	tasksProcessed.WithLabelValues("failure", p.podName).Inc()

	msg.Values["final_error"] = finalErr.Error()
	msg.Values["failed_at"] = time.Now().Format(time.RFC3339)

	args := buildLuaArgs(p.cfg.GroupName, msg.ID, msg.Values)
	err := luaDLQScript.Run(ctx, p.broker.Client, []string{p.cfg.StreamName, p.cfg.DLQStream}, args...).Err()

	if err != nil {
		log.Printf("Error luaDLQScript: %v", err)
	} else {
		log.Printf("Success move on DLQ with taskId %s", msg.ID)
	}
}

package workerpool

import (
	"context"
	"fmt"
	"log"
	"runtime"
	"strconv"
	"sync"
	"sync/atomic"
	"time"

	"github.com/huynhduc2412/DistributedTaskQueue/internal/broker"
	"github.com/huynhduc2412/DistributedTaskQueue/internal/config"
	"github.com/huynhduc2412/DistributedTaskQueue/internal/idempotency"
	"github.com/huynhduc2412/DistributedTaskQueue/internal/task"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/redis/go-redis/v9"
)

var (
	tasksProcessed = prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Name: "queue_tasks_processed_total",
			Help: "Total task processing outcomes.",
		},
		[]string{"status", "pod_name"},
	)
	taskExecutionDuration = prometheus.NewHistogramVec(
		prometheus.HistogramOpts{
			Name:    "queue_task_execution_duration_seconds",
			Help:    "Time spent executing a task and recording its outcome.",
			Buckets: []float64{0.1, 0.25, 0.5, 1, 2, 5, 10, 30},
		},
		[]string{"task_type", "status", "pod_name"},
	)
	tasksReclaimed = prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Name: "queue_tasks_reclaimed_total",
			Help: "Total pending tasks reclaimed after their consumer was idle.",
		},
		[]string{"pod_name"},
	)
	consumerGroupLag = prometheus.NewGaugeVec(
		prometheus.GaugeOpts{
			Name: "queue_consumer_group_lag",
			Help: "Number of stream messages not yet delivered to the consumer group; -1 means Redis cannot determine lag.",
		},
		[]string{"pod_name"},
	)
	pendingMessages = prometheus.NewGaugeVec(
		prometheus.GaugeOpts{
			Name: "queue_pending_messages",
			Help: "Number of messages pending acknowledgement in the consumer group.",
		},
		[]string{"pod_name"},
	)
	dlqMessages = prometheus.NewGaugeVec(
		prometheus.GaugeOpts{
			Name: "queue_dlq_messages",
			Help: "Number of messages currently stored in the dead-letter stream.",
		},
		[]string{"pod_name"},
	)
)

func init() {
	prometheus.MustRegister(
		tasksProcessed,
		taskExecutionDuration,
		tasksReclaimed,
		consumerGroupLag,
		pendingMessages,
		dlqMessages,
	)
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
	cfg              *config.Config
	broker           *broker.RedisBroker
	idempotencyStore idempotency.Store
	taskChan         chan redis.XMessage
	numWorkers       int
	wg               sync.WaitGroup
	podName          string
	successCount     uint64
	failureCount     uint64
	retryCount       uint64
	startTime        time.Time
}

func NewPool(cfs *config.Config, rb *broker.RedisBroker, podName string, store idempotency.Store) *WorkerPool {
	cntWorkers := runtime.GOMAXPROCS(0)
	return &WorkerPool{
		cfg:              cfs,
		broker:           rb,
		idempotencyStore: store,
		numWorkers:       cntWorkers,
		taskChan:         make(chan redis.XMessage, cntWorkers*2),
		podName:          podName,
		startTime:        time.Now(),
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

	p.wg.Add(1)
	go p.reclaimPendingTasks(ctx) // read again stuck task for reasons (crash worker , bug ,...)

	p.wg.Add(1)
	go p.monitorQueueHealth(ctx)

	p.wg.Wait()
	log.Println("workers done tasks!!")
}

func (p *WorkerPool) monitorQueueHealth(ctx context.Context) {
	defer p.wg.Done()

	p.updateQueueHealth(ctx)
	ticker := time.NewTicker(5 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			p.updateQueueHealth(ctx)
		}
	}
}

func (p *WorkerPool) updateQueueHealth(ctx context.Context) {
	groups, err := p.broker.Client.XInfoGroups(ctx, p.cfg.StreamName).Result()
	if err != nil {
		log.Printf("Read consumer group metrics error: %v", err)
	} else {
		for _, group := range groups {
			if group.Name == p.cfg.GroupName {
				consumerGroupLag.WithLabelValues(p.podName).Set(float64(group.Lag))
				break
			}
		}
	}

	pending, err := p.broker.Client.XPending(ctx, p.cfg.StreamName, p.cfg.GroupName).Result()
	if err != nil {
		log.Printf("Read pending message metrics error: %v", err)
	} else {
		pendingMessages.WithLabelValues(p.podName).Set(float64(pending.Count))
	}

	dlqLength, err := p.broker.Client.XLen(ctx, p.cfg.DLQStream).Result()
	if err != nil {
		log.Printf("Read dead-letter queue metrics error: %v", err)
	} else {
		dlqMessages.WithLabelValues(p.podName).Set(float64(dlqLength))
	}
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
					tasksReclaimed.WithLabelValues(p.podName).Inc()
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

	idempotencyKey := taskId
	if jobID, ok := msg.Values["job_id"].(string); ok && jobID != "" {
		idempotencyKey = jobID
	}

	acquired, err := p.idempotencyStore.TryAcquire(ctx, idempotencyKey)
	if err != nil {
		log.Printf("[Worker-%d] idempotency store error for task %s: %v", workerId, idempotencyKey, err)
		_, _ = p.broker.Client.Del(ctx, lockKey).Result()
		return
	}
	if !acquired {
		log.Printf("[Worker-%d] task %s was already processed or is currently being handled; skipping", workerId, idempotencyKey)
		p.broker.Ack(ctx, p.cfg.StreamName, p.cfg.GroupName, msg.ID)
		_, _ = p.broker.Client.Del(ctx, lockKey).Result()
		return
	}

	log.Printf("[Worker-%d] Key obtained successfully. Task processing begins: %s", workerId, taskId)

	taskType := msg.Values["type"].(string)
	// get handler in global registry
	handler, err := task.GetHandler(taskType)

	if err != nil {
		log.Printf("Not found registry for %s", taskType)
		p.broker.Ack(ctx, p.cfg.StreamName, p.cfg.GroupName, msg.ID)
		_, _ = p.broker.Client.Del(ctx, lockKey).Result()
		return
	}

	var handlerErr error
	startedAt := time.Now()

	defer func() {
		if r := recover(); r != nil {
			buf := make([]byte, 1<<16)
			n := runtime.Stack(buf, false)
			handlerErr = fmt.Errorf("panic: %v", r)
			log.Printf("[Worker-%d] panic while handling task %s: %v\n%s", workerId, taskId, r, string(buf[:n]))
		}

		outcome := "success"
		if handlerErr != nil {
			_ = p.idempotencyStore.Release(ctx, idempotencyKey)
			outcome = p.hanldRetry(ctx, msg, handlerErr)
		} else {
			_ = p.idempotencyStore.MarkCompleted(ctx, idempotencyKey)
			atomic.AddUint64(&p.successCount, 1)
			tasksProcessed.WithLabelValues("success", p.podName).Inc()
			p.broker.Ack(ctx, p.cfg.StreamName, p.cfg.GroupName, msg.ID)
		}
		taskExecutionDuration.WithLabelValues(metricTaskType(taskType), outcome, p.podName).Observe(time.Since(startedAt).Seconds())

		_, err := p.broker.Client.Del(ctx, lockKey).Result()
		if err != nil {
			log.Printf("Deleted lockKey for task executed error : %v", err)
		}
	}()

	handlerErr = handler(ctx, msg.Values)
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

func (p *WorkerPool) hanldRetry(ctx context.Context, msg redis.XMessage, err error) string {
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

		_, err := luaRetryScript.Run(ctx, p.broker.Client, []string{p.cfg.StreamName}, args...).Result()

		if err != nil {
			log.Printf("Error luaRetryScript: %v", err)
		} else {
			log.Printf("retry %d times for Task %s", retryCount, msg.ID)
		}
		return "retry"
	} else {
		//move on DLQ
		p.moveToDLQ(ctx, msg, err)
		p.broker.Ack(ctx, p.cfg.StreamName, p.cfg.GroupName, msg.ID)
		return "dlq"
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
	tasksProcessed.WithLabelValues("dlq", p.podName).Inc()

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

func metricTaskType(taskType string) string {
	switch taskType {
	case "EMAIL", "IMAGE":
		return taskType
	default:
		return "UNKNOWN"
	}
}

//go:build integration

package workerpool

import (
	"context"
	"errors"
	"fmt"
	"os"
	"sync/atomic"
	"testing"
	"time"

	"github.com/huynhduc2412/DistributedTaskQueue/internal/broker"
	"github.com/huynhduc2412/DistributedTaskQueue/internal/config"
	"github.com/huynhduc2412/DistributedTaskQueue/internal/idempotency"
	"github.com/huynhduc2412/DistributedTaskQueue/internal/task"
	"github.com/redis/go-redis/v9"
)

func TestWorkerPoolRetryAndIdempotencyIntegration(t *testing.T) {
	redisAddr := os.Getenv("TEST_REDIS_ADDR")
	mysqlDSN := os.Getenv("TEST_MYSQL_DSN")
	if redisAddr == "" || mysqlDSN == "" {
		t.Skip("set TEST_REDIS_ADDR and TEST_MYSQL_DSN to run the integration test")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()

	rb := broker.NewRedisBroker(redisAddr, 1)
	defer rb.Client.Close()
	if err := rb.Client.Ping(ctx).Err(); err != nil {
		t.Fatalf("ping Redis: %v", err)
	}
	suffix := time.Now().UnixNano()
	jobID := fmt.Sprintf("integration-job-%d", suffix)

	store, err := idempotency.NewMySQLStore(mysqlDSN)
	if err != nil {
		t.Fatalf("open idempotency store: %v", err)
	}
	defer store.Release(context.Background(), jobID)

	stream := fmt.Sprintf("workerpool-integration-%d", suffix)
	dlqStream := stream + ":dlq"
	group := "workers"
	consumer := "integration-consumer"
	defer rb.Client.Del(context.Background(), stream, dlqStream)

	if err := rb.Client.XGroupCreateMkStream(ctx, stream, group, "0").Err(); err != nil {
		t.Fatalf("create consumer group: %v", err)
	}

	taskType := fmt.Sprintf("INTEGRATION_%d", suffix)
	defer delete(task.Registry, taskType)
	var attempts atomic.Int32
	task.Register(taskType, func(context.Context, map[string]interface{}) error {
		if attempts.Add(1) == 1 {
			return errors.New("transient handler failure")
		}
		return nil
	})

	p := NewPool(&config.Config{StreamName: stream, GroupName: group, DLQStream: dlqStream}, rb, consumer, store)

	first := publishAndReadOne(t, ctx, rb, stream, group, consumer, taskType, jobID)
	p.exucteTask(ctx, first, 1)

	retried := readOne(t, ctx, rb, stream, group, consumer)
	if got, _ := retried.Values["retry_count"].(string); got != "1" {
		t.Fatalf("retry_count = %q, want 1", got)
	}
	p.exucteTask(ctx, retried, 1)

	if got := attempts.Load(); got != 2 {
		t.Fatalf("handler attempts = %d, want 2", got)
	}
	assertNoPending(t, ctx, rb.Client, stream, group)

	acquired, err := store.TryAcquire(ctx, jobID)
	if err != nil {
		t.Fatalf("check completed idempotency record: %v", err)
	}
	if acquired {
		t.Fatal("completed job was acquired again")
	}

	duplicate := publishAndReadOne(t, ctx, rb, stream, group, consumer, taskType, jobID)
	p.exucteTask(ctx, duplicate, 1)
	if got := attempts.Load(); got != 2 {
		t.Fatalf("duplicate job invoked handler %d times, want 2 total attempts", got)
	}
	assertNoPending(t, ctx, rb.Client, stream, group)
}

func publishAndReadOne(t *testing.T, ctx context.Context, rb *broker.RedisBroker, stream, group, consumer, taskType, jobID string) redis.XMessage {
	t.Helper()
	if err := rb.Publish(ctx, stream, map[string]interface{}{"type": taskType, "job_id": jobID}); err != nil {
		t.Fatalf("publish task: %v", err)
	}
	return readOne(t, ctx, rb, stream, group, consumer)
}

func readOne(t *testing.T, ctx context.Context, rb *broker.RedisBroker, stream, group, consumer string) redis.XMessage {
	t.Helper()
	messages, err := rb.ReadBatch(ctx, stream, group, consumer, 1)
	if err != nil {
		t.Fatalf("read task: %v", err)
	}
	if len(messages) != 1 {
		t.Fatalf("messages read = %d, want 1", len(messages))
	}
	return messages[0]
}

func assertNoPending(t *testing.T, ctx context.Context, client *redis.Client, stream, group string) {
	t.Helper()
	pending, err := client.XPending(ctx, stream, group).Result()
	if err != nil {
		t.Fatalf("read pending count: %v", err)
	}
	if pending.Count != 0 {
		t.Fatalf("pending messages = %d, want 0", pending.Count)
	}
}

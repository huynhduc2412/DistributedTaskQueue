package task

import (
	"context"
	"time"

	"github.com/redis/go-redis/v9"
)

func WithIdempotency(rdb *redis.Client , nxt Handler) Handler {
	return func(ctx context.Context, payload map[string]interface{}) error {
		jobId := payload["job_id"].(string)

		//check duplicate
		ok, err := rdb.SetArgs(ctx, jobId, "processing", redis.SetArgs{
			Mode: "NX",
			TTL:  time.Hour,
		}).Result()
		if err != nil && err != redis.Nil {
			return nil
		}
		if ok == "" {
			return nil
		}
		err = nxt(ctx , payload)
		if err != nil {
			rdb.Del(ctx , jobId)
		}
		return err
	}
}
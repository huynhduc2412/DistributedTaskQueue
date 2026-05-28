package broker

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/redis/go-redis/v9"
)

type RedisBroker struct {
	Client *redis.Client
}

func NewRedisBroker(addr string, poolSize int) *RedisBroker {
	return &RedisBroker{
		Client: redis.NewClient(&redis.Options{Addr: addr, PoolSize: poolSize}),
	}
}

func (b *RedisBroker) Publish(ctx context.Context, stream string, values map[string]interface{}) error {
	redisValues := make(map[string]string)

	for k, v := range values {
		switch val := v.(type) {
		case string:
			redisValues[k] = val
		default:
			bytes, err := json.Marshal(val)
			if err != nil {
				redisValues[k] = fmt.Sprintf("%v", val)
			} else {
				redisValues[k] = string(bytes)
			}
		}
	}

	return b.Client.XAdd(ctx, &redis.XAddArgs{
		Stream: stream,
		Values: redisValues,
	}).Err()
}

func (b *RedisBroker) ReadBatch(ctx context.Context, stream, group, consumer string, count int64) ([]redis.XMessage, error) {
	res, err := b.Client.XReadGroup(ctx, &redis.XReadGroupArgs{
		Group:    group,
		Consumer: consumer,
		Streams:  []string{stream, ">"},
		Count:    count,
		Block:    2 * time.Second,
	}).Result()

	if err != nil {
		if err == redis.Nil {
			return nil, nil
		}
		return nil, err
	}

	if len(res) == 0 {
		return nil, nil
	}

	return res[0].Messages, nil
}

func (b *RedisBroker) Ack(ctx context.Context, stream, group, messageId string) error {
	return b.Client.XAck(ctx, stream, group, messageId).Err()
}
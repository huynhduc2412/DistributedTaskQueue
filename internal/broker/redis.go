package broker

import (
	"context"
	"time"

	"github.com/redis/go-redis/v9"
)
type  RedisBroker struct {
	Client *redis.Client
}

func NewRedisBroker(addr string , poolSize int) *RedisBroker {
	return  &RedisBroker{
		Client: redis.NewClient(&redis.Options{Addr: addr, PoolSize: poolSize}),
	}
}

func(b *RedisBroker) Publish(ctx context.Context , stream string , values map[string]interface{}) error {
	return b.Client.XAdd(ctx , &redis.XAddArgs{Stream: stream , Values: values}).Err()
}

func(b *RedisBroker) ReadBatch(ctx context.Context , stream , group , consumer string , count int64) ([]redis.XMessage , error) {
	res , err := b.Client.XReadGroup(ctx , &redis.XReadGroupArgs{
		Group: group, Consumer: consumer, Streams: []string{stream , ">"},
		Count: count, Block: 2 * time.Second,
	}).Result()
	return res[0].Messages , err
}

func(b *RedisBroker) Ack(ctx context.Context , stream , group , messageId string) error {
	return b.Client.XAck(ctx , stream , group , messageId).Err()
}


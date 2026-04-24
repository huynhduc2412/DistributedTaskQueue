package broker

import (
	"context"

	"github.com/redis/go-redis/v9"
)
type  RedisBroker struct {
	client *redis.Client
}

func NewRedisBroker(addr string) *RedisBroker {
	return  &RedisBroker{
		client: redis.NewClient(&redis.Options{Addr: addr}),
	}
}

//Queue push task
func(b *RedisBroker) Enqueue(ctx context.Context , stream string , taskData map[string]interface{}) error{
	return b.client.XAdd(ctx , &redis.XAddArgs{
		Stream: stream,
		Values: taskData,
	}).Err()
}
//Create Group if not Stream , auto create both of them
func(b *RedisBroker) InitGroup(ctx context.Context , stream , group string) {
	b.client.XGroupCreateMkStream(ctx , stream , group , "0")
}
func(b *RedisBroker) Consume(ctx context.Context , stream , group , consumer string) ([]redis.XMessage , error) {
	res , err := b.client.XReadGroup(ctx , &redis.XReadGroupArgs{
		Group: group,
		Consumer: consumer,
		Streams: []string{stream , ">"},
		Count:  1,
		Block: 0,
	}).Result()

	if err != nil {
		return nil , err
	}

	if len(res) == 0 {
		return nil , nil
	}
	return res[0].Messages , nil
}

func(b *RedisBroker) Ack(ctx context.Context , stream , group , messageId string) error {
	return b.client.XAck(ctx , stream , group , messageId).Err()
}


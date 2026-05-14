package task

import (
	"context"
	"fmt"
	"time"
)

func HandleImage(ctx context.Context, payload map[string]interface{}) error {
	fmt.Printf("Resize ảnh: %v\n", payload["url"])
	time.Sleep(2 * time.Second)
	return nil
}
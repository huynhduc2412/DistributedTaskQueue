package task

import (
	"context"
	"fmt"
	"time"
)

func HandleEmail(ctx context.Context, payload map[string]interface{}) error {
	fmt.Printf("Gửi Email tới: %v\n", payload["to"])
	time.Sleep(2 * time.Second)
	return nil
}
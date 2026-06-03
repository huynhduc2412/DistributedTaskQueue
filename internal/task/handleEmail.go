package task

import (
	"context"
	"fmt"
	"math/rand/v2"
	"time"
)

func HandleEmail(ctx context.Context, payload map[string]interface{}) error {
	//random 2->6s exucute
	time.Sleep(time.Duration(rand.IntN(5) + 2) * time.Second)
	fmt.Printf("Send successfully to : %v\n", payload["to"])
	return nil
}
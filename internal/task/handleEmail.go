package task

import (
	"context"
	// "errors"
	"fmt"
	"math/rand/v2"
	"time"
)

func HandleEmail(ctx context.Context, payload map[string]interface{}) error {
	//random 2->6s exucute
	time.Sleep(time.Duration(rand.IntN(5) + 2) * time.Second)
	// r := rand.IntN(2)
	// if r == 0 {
	// 	return errors.New("random wrong")
	// }
	fmt.Printf("Send successfully to : %v\n", payload["to"])
	return nil
}
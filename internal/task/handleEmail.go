package task

import (
	"context"
	"errors"
	"fmt"
	"math/rand/v2"
	"time"
)

func HandleEmail(ctx context.Context, payload map[string]interface{}) error {
	//random 500ms->1s exucute
	time.Sleep(time.Duration(rand.IntN(501) + 500) * time.Millisecond)
	r := rand.IntN(2)
	if r == 0 {
		return errors.New("random wrong")
	}
	fmt.Printf("Send successfully to : %v\n", payload["to"])
	return nil
}
package task

import (
	"context"
	"fmt"
)

type Handler func(context.Context , map[string]interface{}) error
var Registry = make(map[string]Handler)

func Register(t string , h Handler) {Registry[t] = h}

func GetHandler(taskType string) (Handler , error) {
	h , ok := Registry[taskType]
	if !ok {
		return nil , fmt.Errorf("task %s wasn't registried" , taskType)
	}
	return h , nil
}

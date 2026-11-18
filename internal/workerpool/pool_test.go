package workerpool

import (
	"reflect"
	"testing"
	"time"
)

func TestBuildLuaArgsPreservesPayload(t *testing.T) {
	values := map[string]interface{}{
		"job_id":      "job-123",
		"retry_count": "1",
		"type":        "EMAIL",
	}

	args := buildLuaArgs("workers", "1-0", values)
	if !reflect.DeepEqual(args[:2], []interface{}{"workers", "1-0"}) {
		t.Fatalf("Lua metadata = %#v, want group name and message ID", args[:2])
	}

	gotValues := make(map[string]interface{}, len(values))
	for i := 2; i < len(args); i += 2 {
		key, ok := args[i].(string)
		if !ok {
			t.Fatalf("argument key %d has type %T, want string", i, args[i])
		}
		gotValues[key] = args[i+1]
	}
	if !reflect.DeepEqual(gotValues, values) {
		t.Fatalf("Lua payload = %#v, want %#v", gotValues, values)
	}
}

func TestRetryPolicy(t *testing.T) {
	tests := []struct {
		name    string
		value   interface{}
		count   int
		retryOK bool
	}{
		{name: "missing retry count", count: 0, retryOK: true},
		{name: "first retry", value: "1", count: 1, retryOK: true},
		{name: "documented final requeue", value: "3", count: 3, retryOK: true},
		{name: "fourth requeue allowed by current policy", value: "4", count: 4, retryOK: false},
		{name: "invalid retry count", value: "invalid", count: 0, retryOK: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			values := map[string]interface{}{}
			if tt.value != nil {
				values["retry_count"] = tt.value
			}

			got := retryCountFrom(values)
			if got != tt.count {
				t.Fatalf("retryCountFrom() = %d, want %d", got, tt.count)
			}
			if got := canRetry(got); got != tt.retryOK {
				t.Fatalf("canRetry(%d) = %t, want %t", tt.count, got, tt.retryOK)
			}
		})
	}
}

func TestWorkerStatusUsesPoolState(t *testing.T) {
	startedAt := time.Now().Add(-time.Second)
	p := &WorkerPool{
		numWorkers:   4,
		podName:      "worker-test",
		successCount: 7,
		failureCount: 2,
		retryCount:   3,
		startTime:    startedAt,
	}

	status := p.GetStatus()
	if status.SuccessCount != 7 || status.FailureCount != 2 || status.RetryCount != 3 {
		t.Fatalf("unexpected counters: %#v", status)
	}
	if status.Workers != 4 || status.PodName != "worker-test" {
		t.Fatalf("unexpected worker metadata: %#v", status)
	}
	if status.Uptime == "0s" || status.Uptime == "" {
		t.Fatalf("uptime = %q, want a positive duration", status.Uptime)
	}
}

func TestMetricTaskType(t *testing.T) {
	for _, tt := range []struct {
		input string
		want  string
	}{
		{input: "EMAIL", want: "EMAIL"},
		{input: "IMAGE", want: "IMAGE"},
		{input: "unknown", want: "UNKNOWN"},
	} {
		if got := metricTaskType(tt.input); got != tt.want {
			t.Errorf("metricTaskType(%q) = %q, want %q", tt.input, got, tt.want)
		}
	}
}

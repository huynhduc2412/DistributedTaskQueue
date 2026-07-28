package main

import (
	"net/http"
	"strconv"
	"time"

	"github.com/prometheus/client_golang/prometheus"
)

var (
	producerHTTPRequests = prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Name: "queue_producer_http_requests_total",
			Help: "Total HTTP requests handled by the queue producer.",
		},
		[]string{"status"},
	)
	producerHTTPRequestDuration = prometheus.NewHistogramVec(
		prometheus.HistogramOpts{
			Name:    "queue_producer_http_request_duration_seconds",
			Help:    "Duration of HTTP requests handled by the queue producer.",
			Buckets: prometheus.DefBuckets,
		},
		[]string{"status"},
	)
	tasksEnqueued = prometheus.NewCounter(
		prometheus.CounterOpts{
			Name: "queue_tasks_enqueued_total",
			Help: "Total tasks successfully published to the Redis stream.",
		},
	)
	enqueueFailures = prometheus.NewCounter(
		prometheus.CounterOpts{
			Name: "queue_tasks_enqueue_failures_total",
			Help: "Total failed attempts to publish a task to the Redis stream.",
		},
	)
	admissionRejected = prometheus.NewCounter(
		prometheus.CounterOpts{
			Name: "queue_tasks_admission_rejected_total",
			Help: "Total task submissions rejected because the stream limit was reached.",
		},
	)
)

func init() {
	prometheus.MustRegister(
		producerHTTPRequests,
		producerHTTPRequestDuration,
		tasksEnqueued,
		enqueueFailures,
		admissionRejected,
	)
}

type statusRecorder struct {
	http.ResponseWriter
	statusCode int
}

func (w *statusRecorder) WriteHeader(statusCode int) {
	if w.statusCode != 0 {
		return
	}
	w.statusCode = statusCode
	w.ResponseWriter.WriteHeader(statusCode)
}

func (w *statusRecorder) Write(body []byte) (int, error) {
	if w.statusCode == 0 {
		w.statusCode = http.StatusOK
	}
	return w.ResponseWriter.Write(body)
}

func instrumentProducer(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		startedAt := time.Now()
		recorder := &statusRecorder{ResponseWriter: w}

		next.ServeHTTP(recorder, r)

		if recorder.statusCode == 0 {
			recorder.statusCode = http.StatusOK
		}
		statusCode := strconv.Itoa(recorder.statusCode)
		producerHTTPRequests.WithLabelValues(statusCode).Inc()
		producerHTTPRequestDuration.WithLabelValues(statusCode).Observe(time.Since(startedAt).Seconds())
	})
}

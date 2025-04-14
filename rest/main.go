package main

import (
	"context"
	"encoding/json"
	"fmt"
	"github.com/gofiber/fiber/v2/middleware/adaptor"
	"time"

	"github.com/gofiber/fiber/v2"
	"github.com/google/uuid"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promhttp"
	"github.com/redis/go-redis/v9"
)

var (
	ctx = context.Background()
	rdb *redis.Client

	buckets = []float64{
		0.1, 0.2, 0.5, 1, 2, 3, 4, 5,
		10, 20, 50, 100, 200, 500,
		1000, 2000,
	}

	successCounter = prometheus.NewCounter(prometheus.CounterOpts{
		Name: "rest_success_total",
		Help: "Total number of successful validations",
	})

	failureCounter = prometheus.NewCounter(prometheus.CounterOpts{
		Name: "rest_failure_total",
		Help: "Total number of failed validations",
	})

	queuedGauge = prometheus.NewGauge(prometheus.GaugeOpts{
		Name: "rest_queued_count",
		Help: "Current count of queued validations",
	})

	proxyQueueDurationMs = prometheus.NewHistogram(prometheus.HistogramOpts{
		Name:    "rest_proxy_queue_duration_ms",
		Help:    "Duration from proxy receive to Redis push (ms)",
		Buckets: buckets,
	})

	workerProcessDurationMs = prometheus.NewHistogram(prometheus.HistogramOpts{
		Name:    "rest_worker_process_duration_ms",
		Help:    "Duration from Redis push to worker response (ms)",
		Buckets: buckets,
	})

	proxyWaitDurationMs = prometheus.NewHistogram(prometheus.HistogramOpts{
		Name:    "rest_proxy_wait_duration_ms",
		Help:    "Duration from worker response to proxy response (ms)",
		Buckets: buckets,
	})

	fullCycleDurationMs = prometheus.NewHistogram(prometheus.HistogramOpts{
		Name:    "rest_total_duration_ms",
		Help:    "Full roundtrip duration from receive to response (ms)",
		Buckets: buckets,
	})
)

// --- Data Structures ---

type Meta struct {
	ProxyRequestReceived     *int64 `json:"proxy_request_received_ns"`
	ProxyRequestPushed       *int64 `json:"proxy_request_pushed_ns"`
	WorkerRequestPulled      *int64 `json:"worker_request_pulled_ns"`
	WorkerResponsePushed     *int64 `json:"worker_response_pushed_ns"`
	ProxyResponsePulled      *int64 `json:"proxy_response_pulled_ns"`
	ProxyRoundtripDurationNs *int64 `json:"proxy_roundtrip_duration_ns"`
}

type Data struct {
	Content string `json:"content"`
	Result  bool   `json:"result"`
}

type Message struct {
	RequestID string `json:"request_id"`
	Meta      Meta   `json:"meta"`
	Data      Data   `json:"data"`
}

// --- Utility Functions ---

func nowNs() *int64 {
	t := time.Now().UnixNano()
	return &t
}

// --- Redis Setup ---

func initRedis() {
	rdb = redis.NewClient(&redis.Options{
		Addr: "redis:6379",
	})
}

// --- Fiber App Entry Point ---

func main() {
	initRedis()

	// Register Prometheus metrics
	prometheus.MustRegister(
		successCounter, failureCounter,
		queuedGauge,
		proxyQueueDurationMs, workerProcessDurationMs, proxyWaitDurationMs, fullCycleDurationMs,
	)

	app := fiber.New()

	app.Get("/metrics", adaptor.HTTPHandler(promhttp.Handler()))
	app.Get("/validate", validateHandler)

	fmt.Println("Listening on :3000")
	app.Listen(":3000")
}

// --- Main Controller Handler ---

func validateHandler(c *fiber.Ctx) error {
	input, err := extractContent(c)
	if err != nil {
		return err
	}

	msg := prepareMessage(input)
	logHandling(msg)

	if err := pushToQueue(msg); err != nil {
		failureCounter.Inc()
		return fiber.NewError(fiber.StatusInternalServerError, "Failed to push to job queue")
	}
	queuedGauge.Inc()
	result, err := waitForResult(msg.RequestID)
	queuedGauge.Dec()
	if err != nil {
		failureCounter.Inc()
		return fiber.NewError(fiber.StatusGatewayTimeout, "Timeout waiting for result")
	}

	finalMsg := finalizeResult(result)
	logHandling(finalMsg)

	c.Set("Content-Type", "application/json")
	return c.JSON(finalMsg)
}

// --- Sub-functions used by the controller ---

func extractContent(c *fiber.Ctx) (string, error) {
	input := c.Query("content")
	if input == "" {
		return "", fiber.NewError(fiber.StatusBadRequest, "Missing 'content' query param")
	}
	return input, nil
}

func prepareMessage(content string) *Message {
	now := nowNs()
	return &Message{
		RequestID: uuid.New().String(),
		Meta: Meta{
			ProxyRequestReceived: now,
			ProxyRequestPushed:   nowNs(),
		},
		Data: Data{
			Content: content,
			Result:  false,
		},
	}
}

func pushToQueue(msg *Message) error {
	payload, err := json.Marshal(msg)
	if err != nil {
		return err
	}
	return rdb.RPush(ctx, "validate:queue", payload).Err()
}

func waitForResult(requestId string) (*Message, error) {
	resultKey := fmt.Sprintf("validate:response:%s", requestId)
	result, err := rdb.BLPop(ctx, 5*time.Minute, resultKey).Result()
	if err != nil || len(result) < 2 {
		return nil, err
	}

	var msg Message
	if err := json.Unmarshal([]byte(result[1]), &msg); err != nil {
		return nil, err
	}

	_ = rdb.Del(ctx, resultKey)
	return &msg, nil
}

func logHandling(msg *Message) {
	fmt.Printf("[REST] Handling request_id=%s | content=%q | received_ns=%d\n",
		msg.RequestID,
		msg.Data.Content,
		*msg.Meta.ProxyRequestReceived,
	)
}

func finalizeResult(msg *Message) *Message {
	now := time.Now().UnixNano()
	msg.Meta.ProxyResponsePulled = &now

	// Duration in nanoseconds
	duration := now - *msg.Meta.ProxyRequestReceived
	msg.Meta.ProxyRoundtripDurationNs = &duration

	// Convert to milliseconds for Prometheus
	proxyQueueDurationMs.Observe(float64(*msg.Meta.ProxyRequestPushed-*msg.Meta.ProxyRequestReceived) / 1_000_000)
	workerProcessDurationMs.Observe(float64(*msg.Meta.WorkerResponsePushed-*msg.Meta.ProxyRequestPushed) / 1_000_000)
	proxyWaitDurationMs.Observe(float64(now-*msg.Meta.WorkerResponsePushed) / 1_000_000)
	fullCycleDurationMs.Observe(float64(now-*msg.Meta.ProxyRequestReceived) / 1_000_000)

	// success
	successCounter.Inc()

	return msg
}

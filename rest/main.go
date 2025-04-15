package main

import (
	"context"
	"encoding/json"
	"fmt"
	"github.com/gofiber/fiber/v2/middleware/adaptor"
	"github.com/json-iterator/go"
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

	// --- Metrics ---

	buckets = []float64{
		0.1, 0.2, 0.5, 1, 2, 3, 4, 5,
		10, 20, 50, 100, 200, 500,
		1000, 2000,
	}

	counterSuccess = prometheus.NewCounter(prometheus.CounterOpts{
		Name: "rest_success_total",
		Help: "Total number of successful requests",
	})

	counterFailure = prometheus.NewCounter(prometheus.CounterOpts{
		Name: "rest_failure_total",
		Help: "Total number of failed requests",
	})

	gaugeQueued = prometheus.NewGauge(prometheus.GaugeOpts{
		Name: "rest_queued_count",
		Help: "Unreliable counter of queued requests (may fail on errors)",
	})

	durationRestRequestToRestPushMs = prometheus.NewHistogram(prometheus.HistogramOpts{
		Name:    "duration_rest_request_to_queue_push_ms",
		Help:    "Duration from REST request to Redis push (REST) (ms)",
		Buckets: buckets,
	})

	durationRestPushToWorkerPullMs = prometheus.NewHistogram(prometheus.HistogramOpts{
		Name:    "duration_rest_push_to_worker_pull_ms",
		Help:    "Duration from Redis push (REST) to Redis pull (Worker) (ms)",
		Buckets: buckets,
	})

	durationWorkerPullToWorkerPushMs = prometheus.NewHistogram(prometheus.HistogramOpts{
		Name:    "duration_worker_pull_to_worker_push_ms",
		Help:    "Duration from Redis pull (Worker) to Redis push (Worker) (ms)",
		Buckets: buckets,
	})

	durationWorkerPushToRestPullMs = prometheus.NewHistogram(prometheus.HistogramOpts{
		Name:    "duration_worker_push_to_rest_pull_ms",
		Help:    "Duration from Redis push (Worker) to Redis pull (REST) (ms)",
		Buckets: buckets,
	})

	durationRestPullToRestResponseMs = prometheus.NewHistogram(prometheus.HistogramOpts{
		Name:    "duration_rest_pull_to_rest_response_ms",
		Help:    "Duration from Redis pull (REST) to HTTP response (REST) (ms)",
		Buckets: buckets,
	})

	durationFullCycleMs = prometheus.NewHistogram(prometheus.HistogramOpts{
		Name:    "duration_total_roundtrip_ms",
		Help:    "Total roundtrip time from REST request to REST response (ms)",
		Buckets: buckets,
	})
)

// --- Data Structures ---

type Meta struct {
	RestRequestReceived  *int64 `json:"rest_request_received_ns"`
	RestRequestPushed    *int64 `json:"rest_request_pushed_ns"`
	WorkerRequestPulled  *int64 `json:"worker_request_pulled_ns"`
	WorkerResponsePushed *int64 `json:"worker_response_pushed_ns"`
	RestResponsePulled   *int64 `json:"rest_response_pulled_ns"`
	RoundtripDurationNs  *int64 `json:"rest_roundtrip_duration_ns"`
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
		Addr:     "redis:6379",
		PoolSize: 80,
	})
}

// --- Fiber App Entry Point ---

func main() {
	initRedis()

	// Register Prometheus metrics
	prometheus.MustRegister(
		counterSuccess,                   // Operation success counters
		counterFailure,                   // Operation failed counters
		gaugeQueued,                      // Unreliable counter of queued requests (may fail on errors)
		durationRestRequestToRestPushMs,  // From REST request receive → Redis push (by REST)
		durationRestPushToWorkerPullMs,   // From Redis push (REST) → Redis pull (Worker)
		durationWorkerPullToWorkerPushMs, // From Redis pull (Worker) → Redis push (Worker)
		durationWorkerPushToRestPullMs,   // From Redis push (Worker) → Redis pull (REST)
		durationRestPullToRestResponseMs, // From Redis pull (REST) → HTTP response (REST)
		durationFullCycleMs,              // Full roundtrip: REST request → HTTP response
	)

	app := fiber.New()

	app.Get("/metrics", adaptor.HTTPHandler(promhttp.Handler()))
	app.Get("/validate", validateHandler)

	fmt.Println("Listening on :3000")
	app.Listen(":3000")
}

// --- Main Controller Handler ---

func validateHandler(c *fiber.Ctx) error {
	requestReceived := nowNs()
	input, err := extractContent(c)
	if err != nil {
		return err
	}

	msg := prepareMessage(input, requestReceived)
	logHandling(msg)

	if err := pushToQueue(msg); err != nil {
		counterFailure.Inc()
		return fiber.NewError(fiber.StatusInternalServerError, "Failed to push to job queue")
	}
	gaugeQueued.Inc()
	result, err := waitForResult(msg.RequestID)
	gaugeQueued.Dec()
	if err != nil {
		counterFailure.Inc()
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

func prepareMessage(content string, requestReceived *int64) *Message {
	return &Message{
		RequestID: uuid.New().String(),
		Meta: Meta{
			RestRequestReceived: requestReceived,
			RestRequestPushed:   nowNs(),
		},
		Data: Data{
			Content: content,
			Result:  false,
		},
	}
}

func pushToQueue(msg *Message) error {
	payload, err := jsoniter.Marshal(msg)
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
		*msg.Meta.RestRequestReceived,
	)
}

func finalizeResult(msg *Message) *Message {
	now := time.Now().UnixNano()
	msg.Meta.RestResponsePulled = &now

	// Compute and store total roundtrip duration
	duration := now - *msg.Meta.RestRequestReceived
	msg.Meta.RoundtripDurationNs = &duration

	// Observe Prometheus histograms (in ms)
	durationRestRequestToRestPushMs.Observe(float64(*msg.Meta.RestRequestPushed-*msg.Meta.RestRequestReceived) / 1_000_000)
	durationRestPushToWorkerPullMs.Observe(float64(*msg.Meta.WorkerRequestPulled-*msg.Meta.RestRequestPushed) / 1_000_000)
	durationWorkerPullToWorkerPushMs.Observe(float64(*msg.Meta.WorkerResponsePushed-*msg.Meta.WorkerRequestPulled) / 1_000_000)
	durationWorkerPushToRestPullMs.Observe(float64(*msg.Meta.RestResponsePulled-*msg.Meta.WorkerResponsePushed) / 1_000_000)
	durationRestPullToRestResponseMs.Observe(float64(now-*msg.Meta.RestResponsePulled) / 1_000_000)
	durationFullCycleMs.Observe(float64(duration) / 1_000_000)

	// Mark success
	counterSuccess.Inc()

	return msg
}

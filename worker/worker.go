package main

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/redis/go-redis/v9"
)

var ctx = context.Background()

// --- Structures match the REST side with *_ns fields ---

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

func nowNs() *int64 {
	ns := time.Now().UnixNano()
	return &ns
}

func main() {
	rdb := redis.NewClient(&redis.Options{
		Addr: "redis:6379",
	})

	for {
		// Blocking pop from validation queue
		result, err := rdb.BLPop(ctx, 0, "validate:queue").Result()
		if err != nil {
			fmt.Println("Queue error:", err)
			continue
		}

		var msg Message
		if err := json.Unmarshal([]byte(result[1]), &msg); err != nil {
			fmt.Println("Invalid message:", err)
			continue
		}

		msg.Meta.WorkerRequestPulled = nowNs()

		// Simulate processing
		msg.Data.Content = strings.ToUpper(msg.Data.Content)
		msg.Data.Result = true

		msg.Meta.WorkerResponsePushed = nowNs()

		// Push response to result queue with 1-hour expiration
		resultKey := fmt.Sprintf("validate:response:%s", msg.RequestID)
		serialized, _ := json.Marshal(msg)

		if err := rdb.RPush(ctx, resultKey, serialized).Err(); err != nil {
			fmt.Println("Failed to push result:", err)
			continue
		}

		rdb.Expire(ctx, resultKey, time.Hour)

		fmt.Println("Processed:", msg.RequestID)
	}
}

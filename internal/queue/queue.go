// Package queue handles messaging queues using Redis list structures.
package queue

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/redis/go-redis/v9"
)

// TaskPayload represents the message payload passed through the task queue.
type TaskPayload struct {
	RunID    int64  `json:"run_id"`    // The unique ID of the workflow run
	TaskName string `json:"task_name"` // The name of the task step to execute
	Attempt  int    `json:"attempt"`   // The current retry attempt count
}

// Queue defines the messaging queue interface for managing workflow steps.
type Queue interface {
	// Enqueue appends a task execution message to the queue.
	Enqueue(ctx context.Context, payload TaskPayload) error

	// Dequeue blocks until a task message is available and retrieves it.
	Dequeue(ctx context.Context) (*TaskPayload, error)

	// EnqueueScheduled schedules a task message to be enqueued after a specific delay.
	EnqueueScheduled(ctx context.Context, payload TaskPayload, delay time.Duration) error
}

// RedisQueue implements a Redis list-backed Queue.
type RedisQueue struct {
	client *redis.Client
	key    string
}

// NewRedisQueue constructs a new RedisQueue.
func NewRedisQueue(client *redis.Client, key string) *RedisQueue {
	if key == "" {
		key = "orchid:task_queue"
	}
	return &RedisQueue{
		client: client,
		key:    key,
	}
}

// Enqueue serialises a TaskPayload to JSON and pushes it to the tail of the list.
func (q *RedisQueue) Enqueue(ctx context.Context, payload TaskPayload) error {
	data, err := json.Marshal(payload)
	if err != nil {
		return fmt.Errorf("failed to marshal task payload: %w", err)
	}

	err = q.client.RPush(ctx, q.key, data).Err()
	if err != nil {
		return fmt.Errorf("failed to enqueue payload to Redis: %w", err)
	}
	return nil
}

// Dequeue blocks using BLPop with 1 second timeouts to ensure it reacts promptly to context cancellations.
func (q *RedisQueue) Dequeue(ctx context.Context) (*TaskPayload, error) {
	for {
		// Verify context hasn't been cancelled before blocking
		if err := ctx.Err(); err != nil {
			return nil, err
		}

		// Perform blocking pop with 1 second timeout
		results, err := q.client.BLPop(ctx, 1*time.Second, q.key).Result()
		if err != nil {
			if errors.Is(err, redis.Nil) {
				continue // Timeout reached with no items, retry pop
			}
			return nil, err // Return Canceled or connection errors immediately
		}

		// BLPop returns [list_key, element_value]
		if len(results) < 2 {
			return nil, fmt.Errorf("invalid blpop result structure: %v", results)
		}

		var payload TaskPayload
		err = json.Unmarshal([]byte(results[1]), &payload)
		if err != nil {
			return nil, fmt.Errorf("failed to unmarshal task payload: %w", err)
		}

		return &payload, nil
	}
}

// EnqueueScheduled serializes the payload to JSON and pushes it to a Redis sorted set with score set to unix time + delay.
func (q *RedisQueue) EnqueueScheduled(ctx context.Context, payload TaskPayload, delay time.Duration) error {
	data, err := json.Marshal(payload)
	if err != nil {
		return fmt.Errorf("failed to marshal task payload: %w", err)
	}

	scheduledKey := q.key + ":scheduled"
	runTime := time.Now().Add(delay).Unix()

	err = q.client.ZAdd(ctx, scheduledKey, redis.Z{
		Score:  float64(runTime),
		Member: data,
	}).Err()
	if err != nil {
		return fmt.Errorf("failed to enqueue scheduled task: %w", err)
	}

	return nil
}

// StartScheduler runs a poller in the background that moves ready tasks from the scheduled ZSET to the task LIST.
// It runs until the context is cancelled.
func (q *RedisQueue) StartScheduler(ctx context.Context) {
	ticker := time.NewTicker(500 * time.Millisecond)
	defer ticker.Stop()

	scheduledKey := q.key + ":scheduled"

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			// Fetch elements from ZSET with score <= current time
			now := time.Now().Unix()
			opt := redis.ZRangeBy{
				Min: "-inf",
				Max: fmt.Sprintf("%d", now),
			}
			items, err := q.client.ZRangeByScore(ctx, scheduledKey, &opt).Result()
			if err != nil || len(items) == 0 {
				continue
			}

			for _, item := range items {
				// Atomically remove and enqueue to prevent duplicate processing by concurrent schedulers
				removed, err := q.client.ZRem(ctx, scheduledKey, item).Result()
				if err == nil && removed > 0 {
					_ = q.client.RPush(ctx, q.key, item).Err()
				}
			}
		}
	}
}

package queue

import (
	"context"
	"os"
	"testing"
	"time"

	"github.com/redis/go-redis/v9"
)

func TestRedisQueue_Integration(t *testing.T) {
	// Use local Redis container for testing, fallback to localhost:6379
	redisAddr := os.Getenv("REDIS_ADDR")
	if redisAddr == "" {
		redisAddr = "localhost:6379"
	}

	client := redis.NewClient(&redis.Options{
		Addr: redisAddr,
	})

	// Quick ping to check if Redis is reachable. If not, skip the integration test.
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	if err := client.Ping(ctx).Err(); err != nil {
		t.Skipf("Skipping Redis integration test. Redis is not reachable at %s: %v", redisAddr, err)
	}

	testQueueKey := "test:orchid:task_queue"
	// Ensure we start with a clean list
	client.Del(context.Background(), testQueueKey)
	defer client.Del(context.Background(), testQueueKey)

	q := NewRedisQueue(client, testQueueKey)

	// 1. Test Enqueue and Dequeue
	payload := TaskPayload{
		RunID:    999,
		TaskName: "sleep",
		Attempt:  2,
	}

	err := q.Enqueue(context.Background(), payload)
	if err != nil {
		t.Fatalf("Enqueue failed: %v", err)
	}

	fetched, err := q.Dequeue(context.Background())
	if err != nil {
		t.Fatalf("Dequeue failed: %v", err)
	}

	if fetched == nil || fetched.RunID != 999 || fetched.TaskName != "sleep" || fetched.Attempt != 2 {
		t.Errorf("unexpected payload values: %+v", fetched)
	}

	// 2. Test Dequeue blocks and can be cancelled via Context
	// We start a dequeue operation in a separate goroutine and cancel the context.
	cancelCtx, cancelFunc := context.WithCancel(context.Background())
	
	errChan := make(chan error, 1)
	go func() {
		_, err := q.Dequeue(cancelCtx)
		errChan <- err
	}()

	// Wait 50ms and cancel the context
	time.Sleep(50 * time.Millisecond)
	cancelFunc()

	select {
	case err := <-errChan:
		if err == nil {
			t.Error("expected error from context cancellation, got nil")
		} else if err != context.Canceled {
			t.Errorf("expected context.Canceled, got: %v", err)
		}
	case <-time.After(2 * time.Second):
		t.Error("dequeue did not unblock after context cancellation within timeout")
	}

	// 3. Test EnqueueScheduled and StartScheduler
	scheduledKey := testQueueKey + ":scheduled"
	client.Del(context.Background(), scheduledKey)
	defer client.Del(context.Background(), scheduledKey)

	schedPayload := TaskPayload{
		RunID:    888,
		TaskName: "hello",
		Attempt:  1,
	}

	// Enqueue with a 200ms delay
	err = q.EnqueueScheduled(context.Background(), schedPayload, 200*time.Millisecond)
	if err != nil {
		t.Fatalf("EnqueueScheduled failed: %v", err)
	}

	// Start scheduler in background
	schedCtx, schedCancel := context.WithCancel(context.Background())
	defer schedCancel()
	go q.StartScheduler(schedCtx)

	// Fetch from queue (blocking pop should receive it once scheduler triggers after 200ms)
	startTime := time.Now()
	fetchedSched, err := q.Dequeue(context.Background())
	if err != nil {
		t.Fatalf("failed to dequeue scheduled item: %v", err)
	}

	duration := time.Since(startTime)
	if duration < 180*time.Millisecond {
		t.Errorf("expected task to be delayed by ~200ms, but was fetched in %v", duration)
	}

	if fetchedSched == nil || fetchedSched.RunID != 888 || fetchedSched.TaskName != "hello" || fetchedSched.Attempt != 1 {
		t.Errorf("unexpected scheduled payload values: %+v", fetchedSched)
	}
}

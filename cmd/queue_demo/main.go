// Command queue_demo provides a simple CLI to push and pop tasks to/from the Redis queue.
package main

import (
	"context"
	"flag"
	"fmt"
	"log"
	"os"

	"orchid/internal/queue"
	"orchid/pkg/utils"

	"github.com/redis/go-redis/v9"
)

func main() {
	utils.LoadEnv()
	redisAddr := os.Getenv("REDIS_ADDR")
	if redisAddr == "" {
		redisAddr = "localhost:6379"
	}

	pushFlag := flag.Bool("push", false, "Push a sample task payload to the queue")
	popFlag := flag.Bool("pop", false, "Blocking pop a task payload from the queue")
	flag.Parse()

	client := redis.NewClient(&redis.Options{
		Addr: redisAddr,
	})

	ctx := context.Background()
	q := queue.NewRedisQueue(client, "orchid:task_queue")

	if *pushFlag {
		payload := queue.TaskPayload{
			RunID:    101,
			TaskName: "hello",
			Attempt:  0,
		}
		err := q.Enqueue(ctx, payload)
		if err != nil {
			log.Fatalf("Enqueue failed: %v", err)
		}
		fmt.Printf("Successfully enqueued: %+v\n", payload)
	} else if *popFlag {
		fmt.Println("Waiting for messages (blocking pop)...")
		payload, err := q.Dequeue(ctx)
		if err != nil {
			log.Fatalf("Dequeue failed: %v", err)
		}
		fmt.Printf("Successfully dequeued: %+v\n", payload)
	} else {
		flag.Usage()
	}
}

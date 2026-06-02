package tasks

import (
	"context"
	"fmt"
	"log"
	"time"

	"orchid/internal/domain"
)

// HelloTask is a mock step that greets the user and logs parameters.
type HelloTask struct{}

func (t *HelloTask) Name() string {
	return "hello"
}

func (t *HelloTask) Execute(ctx context.Context, in domain.TaskInput) (domain.TaskOutput, error) {
	log.Printf("[HelloTask] Running step for RunID: %d, UserID: %s", in.RunID, in.UserID)

	outData := make(map[string]any)
	for k, v := range in.Data {
		outData[k] = v
	}
	outData["hello_message"] = fmt.Sprintf("Hello, User %s!", in.UserID)

	return domain.TaskOutput{Data: outData}, nil
}

// SleepTask is a mock step that simulates work by sleeping.
type SleepTask struct {
	Duration time.Duration
}

func (t *SleepTask) Name() string {
	return "sleep"
}

func (t *SleepTask) Execute(ctx context.Context, in domain.TaskInput) (domain.TaskOutput, error) {
	log.Printf("[SleepTask] Step starting. Sleeping for %v...", t.Duration)

	select {
	case <-time.After(t.Duration):
	case <-ctx.Done():
		log.Printf("[SleepTask] Sleep interrupted: context cancelled")
		return domain.TaskOutput{}, ctx.Err()
	}

	log.Printf("[SleepTask] Step completed.")

	outData := make(map[string]any)
	for k, v := range in.Data {
		outData[k] = v
	}
	outData["slept_duration_ms"] = float64(t.Duration.Milliseconds())

	return domain.TaskOutput{Data: outData}, nil
}

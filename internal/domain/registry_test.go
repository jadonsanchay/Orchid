package domain_test

import (
	"context"
	"testing"
	"time"

	"orchid/internal/domain"
	"orchid/internal/tasks"
)

func TestTaskRegistryAndMockExecution(t *testing.T) {
	registry := domain.NewRegistry()

	hello := &tasks.HelloTask{}
	sleep := &tasks.SleepTask{Duration: 10 * time.Millisecond}

	// 1. Test Register
	err := registry.Register(hello)
	if err != nil {
		t.Fatalf("failed to register hello task: %v", err)
	}

	err = registry.Register(sleep)
	if err != nil {
		t.Fatalf("failed to register sleep task: %v", err)
	}

	// Test double registration error
	err = registry.Register(hello)
	if err == nil {
		t.Error("expected error when registering duplicate task name, got nil")
	}

	// 2. Test Get
	tHello := registry.Get("hello")
	if tHello == nil || tHello.Name() != "hello" {
		t.Errorf("failed to retrieve hello task from registry")
	}

	tSleep := registry.Get("sleep")
	if tSleep == nil || tSleep.Name() != "sleep" {
		t.Errorf("failed to retrieve sleep task from registry")
	}

	tMissing := registry.Get("non-existent")
	if tMissing != nil {
		t.Errorf("expected nil for missing task, got: %v", tMissing)
	}

	// 3. Test Execute Hello
	input := domain.TaskInput{
		RunID:  1,
		UserID: "user-abc",
		Data:   map[string]any{"initial_param": "value1"},
	}

	outHello, err := tHello.Execute(context.Background(), input)
	if err != nil {
		t.Fatalf("hello task execution failed: %v", err)
	}
	if outHello.Data["initial_param"] != "value1" {
		t.Errorf("hello task output missed carryover input parameter")
	}
	if outHello.Data["hello_message"] != "Hello, User user-abc!" {
		t.Errorf("hello task output greeting incorrect: %v", outHello.Data["hello_message"])
	}

	// 4. Test Execute Sleep
	outSleep, err := tSleep.Execute(context.Background(), input)
	if err != nil {
		t.Fatalf("sleep task execution failed: %v", err)
	}
	if outSleep.Data["slept_duration_ms"] != float64(10) {
		t.Errorf("expected sleep duration to be 10ms, got: %v", outSleep.Data["slept_duration_ms"])
	}
}

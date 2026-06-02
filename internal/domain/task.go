// Package domain holds core domain interfaces and workflow orchestrator models.
package domain

import "context"

// TaskInput represents the input payload passed to a workflow task step.
type TaskInput struct {
	RunID  int64          // The unique ID of the workflow run
	UserID string         // The ID of the user triggering the workflow
	Data   map[string]any // Key-value results carried over from previous pipeline steps
}

// TaskOutput represents the resulting payload returned by a workflow task step.
type TaskOutput struct {
	Data map[string]any // Key-value outputs to be persisted and passed to the next step
}

// Task is the interface that every pluggable step in the orchestrator pipeline must satisfy.
type Task interface {
	// Name returns the unique registration identifier of the task.
	Name() string

	// Execute runs the core business logic of the task step.
	Execute(ctx context.Context, in TaskInput) (TaskOutput, error)
}

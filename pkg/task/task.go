package task

import "context"

// Task defines the interface for an isolated, pluggable, and fault-tolerant step 
// in the Orchid workflow orchestration engine. Each task takes a raw byte array
// input (typically JSON), runs its execution logic, and returns a raw byte array
// output (typically JSON) or an error.
type Task interface {
	// Name returns the identifier of the task.
	Name() string

	// Execute runs the task's business logic.
	Execute(ctx context.Context, input []byte) (output []byte, err error)
}

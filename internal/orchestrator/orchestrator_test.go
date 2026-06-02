package orchestrator

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"testing"
	"time"

	"orchid/internal/domain"
	"orchid/internal/queue"
	"orchid/internal/store"
	"orchid/internal/tasks"
)

// MockQueue provides an in-memory implementation of queue.Queue for orchestrator unit testing.
type MockQueue struct {
	mu       sync.Mutex
	payloads []queue.TaskPayload
	cond     *sync.Cond
}

func NewMockQueue() *MockQueue {
	mq := &MockQueue{}
	mq.cond = sync.NewCond(&mq.mu)
	return mq
}

func (mq *MockQueue) Enqueue(ctx context.Context, p queue.TaskPayload) error {
	mq.mu.Lock()
	defer mq.mu.Unlock()
	mq.payloads = append(mq.payloads, p)
	mq.cond.Signal()
	return nil
}

func (mq *MockQueue) Dequeue(ctx context.Context) (*queue.TaskPayload, error) {
	mq.mu.Lock()
	defer mq.mu.Unlock()

	for len(mq.payloads) == 0 {
		// Wait for signal or context cancellation
		released := make(chan struct{})
		go func() {
			select {
			case <-ctx.Done():
				mq.mu.Lock()
				mq.cond.Broadcast()
				mq.mu.Unlock()
			case <-released:
			}
		}()

		mq.cond.Wait()
		close(released)

		if err := ctx.Err(); err != nil {
			return nil, err
		}
	}

	p := mq.payloads[0]
	mq.payloads = mq.payloads[1:]
	return &p, nil
}

func (mq *MockQueue) EnqueueScheduled(ctx context.Context, p queue.TaskPayload, delay time.Duration) error {
	// Simulate scheduled delay in-memory for unit tests
	go func() {
		select {
		case <-time.After(delay):
			_ = mq.Enqueue(context.Background(), p)
		case <-ctx.Done():
		}
	}()
	return nil
}

// MockWorkflowStore provides an in-memory store.WorkflowStore for unit tests.
type MockWorkflowStore struct {
	mu        sync.Mutex
	runs      map[int64]*store.WorkflowRun
	taskRuns  map[string]*store.TaskRun // Key: runID:taskName
	nextRunID int64
}

func NewMockWorkflowStore() *MockWorkflowStore {
	return &MockWorkflowStore{
		runs:      make(map[int64]*store.WorkflowRun),
		taskRuns:  make(map[string]*store.TaskRun),
		nextRunID: 1,
	}
}

func (ms *MockWorkflowStore) CreateRun(ctx context.Context, userID string, workflowType string) (*store.WorkflowRun, error) {
	ms.mu.Lock()
	defer ms.mu.Unlock()

	run := &store.WorkflowRun{
		ID:           ms.nextRunID,
		UserID:       userID,
		WorkflowType: workflowType,
		Status:       "pending",
		CreatedAt:    time.Now(),
		UpdatedAt:    time.Now(),
	}
	ms.runs[run.ID] = run
	ms.nextRunID++
	return run, nil
}

func (ms *MockWorkflowStore) GetRun(ctx context.Context, runID int64) (*store.WorkflowRun, error) {
	ms.mu.Lock()
	defer ms.mu.Unlock()
	return ms.runs[runID], nil
}

func (ms *MockWorkflowStore) UpdateRunStatus(ctx context.Context, runID int64, status string, currentStep string) error {
	ms.mu.Lock()
	defer ms.mu.Unlock()

	run, exists := ms.runs[runID]
	if !exists {
		return errors.New("run not found")
	}
	run.Status = status
	run.CurrentStep = currentStep
	run.UpdatedAt = time.Now()
	return nil
}

func (ms *MockWorkflowStore) CreateTaskRun(ctx context.Context, runID int64, taskName string) (*store.TaskRun, error) {
	ms.mu.Lock()
	defer ms.mu.Unlock()

	key := fmt.Sprintf("%d:%s", runID, taskName)
	task := &store.TaskRun{
		ID:           int64(len(ms.taskRuns) + 1),
		RunID:        runID,
		TaskName:     taskName,
		Status:       "pending",
		AttemptCount: 0,
		UpdatedAt:    time.Now(),
	}
	ms.taskRuns[key] = task
	return task, nil
}

func (ms *MockWorkflowStore) GetTaskRun(ctx context.Context, runID int64, taskName string) (*store.TaskRun, error) {
	ms.mu.Lock()
	defer ms.mu.Unlock()

	key := fmt.Sprintf("%d:%s", runID, taskName)
	return ms.taskRuns[key], nil
}

func (ms *MockWorkflowStore) UpdateTaskRun(ctx context.Context, runID int64, taskName string, status string, attemptCount int, lastError string, output map[string]any) error {
	ms.mu.Lock()
	defer ms.mu.Unlock()

	key := fmt.Sprintf("%d:%s", runID, taskName)
	task, exists := ms.taskRuns[key]
	if !exists {
		return errors.New("task run not found")
	}
	task.Status = status
	task.AttemptCount = attemptCount
	if lastError != "" {
		task.LastError = &lastError
	} else {
		task.LastError = nil
	}
	task.Output = output
	task.UpdatedAt = time.Now()
	return nil
}

func (ms *MockWorkflowStore) ListRuns(ctx context.Context) ([]store.WorkflowRun, error) {
	ms.mu.Lock()
	defer ms.mu.Unlock()

	var list []store.WorkflowRun
	for _, r := range ms.runs {
		list = append(list, *r)
	}
	return list, nil
}

func (ms *MockWorkflowStore) GetIncompleteTasks(ctx context.Context) ([]store.TaskRun, error) {
	ms.mu.Lock()
	defer ms.mu.Unlock()

	var list []store.TaskRun
	for _, tr := range ms.taskRuns {
		run, exists := ms.runs[tr.RunID]
		if exists && run.Status == "running" && (tr.Status == "running" || tr.Status == "pending" || tr.Status == "retrying") {
			list = append(list, *tr)
		}
	}
	return list, nil
}

// GetTaskRunsForRun retrieves all mocked task run executions for a given workflow run.
func (ms *MockWorkflowStore) GetTaskRunsForRun(ctx context.Context, runID int64) ([]store.TaskRun, error) {
	ms.mu.Lock()
	defer ms.mu.Unlock()

	var list []store.TaskRun
	for _, tr := range ms.taskRuns {
		if tr.RunID == runID {
			list = append(list, *tr)
		}
	}
	return list, nil
}

func TestEngine_LinearWorkflowExecution(t *testing.T) {
	// Setup core services
	mockStore := NewMockWorkflowStore()
	mockQueue := NewMockQueue()
	registry := domain.NewRegistry()

	// Register mock tasks
	helloTask := &tasks.HelloTask{}
	sleepTask := &tasks.SleepTask{Duration: 5 * time.Millisecond}
	_ = registry.Register(helloTask)
	_ = registry.Register(sleepTask)

	engine := NewEngine(mockStore, mockQueue, registry)

	// Configure workflow plan
	plan := WorkflowPlan{
		Steps: []string{"hello", "sleep"},
	}
	engine.RegisterPlan("job-search-mock", plan)

	// Start engine in background with context cancellation
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	go engine.Start(ctx, 2) // Run with 2 concurrent workers
	time.Sleep(20 * time.Millisecond) // Allow startup recovery scanner to finish

	// Submit workflow run
	runID, err := engine.SubmitRun(context.Background(), "user-456", "job-search-mock", nil)
	if err != nil {
		t.Fatalf("SubmitRun failed: %v", err)
	}

	// Wait for the pipeline to finish processing
	// A simple timeout loop checking status from the mockStore:
	var finalRun *store.WorkflowRun
	success := false
	for i := 0; i < 20; i++ {
		time.Sleep(15 * time.Millisecond)
		finalRun, _ = mockStore.GetRun(context.Background(), runID)
		if finalRun != nil && finalRun.Status == "completed" {
			success = true
			break
		}
	}

	if !success {
		t.Fatalf("workflow failed to complete within timeout. Final status: %v", finalRun)
	}

	// Verify task checkpoints
	helloState, _ := mockStore.GetTaskRun(context.Background(), runID, "hello")
	if helloState == nil || helloState.Status != "completed" || helloState.AttemptCount != 1 {
		t.Errorf("hello task checkpoint incorrect: %+v", helloState)
	}

	sleepState, _ := mockStore.GetTaskRun(context.Background(), runID, "sleep")
	if sleepState == nil || sleepState.Status != "completed" || sleepState.AttemptCount != 1 {
		t.Errorf("sleep task checkpoint incorrect: %+v", sleepState)
	}

	// Check if state/data carries forward correctly
	if sleepState.Output["hello_message"] != "Hello, User user-456!" {
		t.Errorf("state carryover data failed. Expected greeting, got: %v", sleepState.Output["hello_message"])
	}
	if sleepState.Output["slept_duration_ms"] == nil {
		t.Error("sleep task did not append execution output parameters")
	}
}

// FailingTask mocks a task that fails a specified number of times before succeeding.
type FailingTask struct {
	mu          sync.Mutex
	CallCount   int
	MaxFailures int
}

// Name returns the identifier of the failing task.
func (f *FailingTask) Name() string { return "failing_task" }

// Execute increments the execution attempt count and returns an error for the configured max transient failures before succeeding.
func (f *FailingTask) Execute(ctx context.Context, input domain.TaskInput) (domain.TaskOutput, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.CallCount++
	if f.CallCount <= f.MaxFailures {
		return domain.TaskOutput{}, errors.New("transient error")
	}
	return domain.TaskOutput{Data: map[string]any{"status": "ok"}}, nil
}

// TestEngine_CrashRecovery verifies that unfinished tasks are scanned and re-enqueued on engine start.
func TestEngine_CrashRecovery(t *testing.T) {
	mockStore := NewMockWorkflowStore()
	mockQueue := NewMockQueue()
	registry := domain.NewRegistry()

	engine := NewEngine(mockStore, mockQueue, registry)

	// Pre-populate mock store with an incomplete run
	run, _ := mockStore.CreateRun(context.Background(), "user-123", "job-search-mock")
	_ = mockStore.UpdateRunStatus(context.Background(), run.ID, "running", "hello")
	
	// Create an incomplete task run (in progress/running)
	_, _ = mockStore.CreateTaskRun(context.Background(), run.ID, "hello")
	_ = mockStore.UpdateTaskRun(context.Background(), run.ID, "hello", "running", 1, "", nil)

	ctx, cancel := context.WithCancel(context.Background())
	// Cancel immediately to trigger the scanner and immediately stop the engine
	cancel()
	engine.Start(ctx, 1)

	// Assert the task was re-enqueued by the scanner
	mockQueue.mu.Lock()
	defer mockQueue.mu.Unlock()
	if len(mockQueue.payloads) != 1 {
		t.Fatalf("expected 1 task payload to be recovered, got %d", len(mockQueue.payloads))
	}
	payload := mockQueue.payloads[0]
	if payload.RunID != run.ID || payload.TaskName != "hello" || payload.Attempt != 1 {
		t.Errorf("recovered task payload mismatch: %+v", payload)
	}
}

// TestEngine_RetriesWithBackoff verifies that failed tasks are retried with scheduled backoff and succeed if attempts remain.
func TestEngine_RetriesWithBackoff(t *testing.T) {
	mockStore := NewMockWorkflowStore()
	mockQueue := NewMockQueue()
	registry := domain.NewRegistry()

	failingTask := &FailingTask{MaxFailures: 2}
	_ = registry.Register(failingTask)

	engine := NewEngine(mockStore, mockQueue, registry)
	engine.RegisterPlan("retry-test", WorkflowPlan{Steps: []string{"failing_task"}})
	engine.RegisterRetryPolicy("failing_task", RetryPolicy{
		MaxAttempts: 3,
		Backoff:     5 * time.Millisecond,
	})

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	go engine.Start(ctx, 2)
	time.Sleep(20 * time.Millisecond) // Allow startup recovery scanner to finish

	runID, err := engine.SubmitRun(context.Background(), "user-789", "retry-test", nil)
	if err != nil {
		t.Fatalf("SubmitRun failed: %v", err)
	}

	// Wait for completion
	var finalRun *store.WorkflowRun
	success := false
	for i := 0; i < 20; i++ {
		time.Sleep(15 * time.Millisecond)
		finalRun, _ = mockStore.GetRun(context.Background(), runID)
		if finalRun != nil && finalRun.Status == "completed" {
			success = true
			break
		}
	}

	if !success {
		t.Fatalf("workflow failed to complete. Final run state: %+v", finalRun)
	}

	failingTask.mu.Lock()
	calls := failingTask.CallCount
	failingTask.mu.Unlock()

	if calls != 3 {
		t.Errorf("expected task to be called 3 times (2 fails, 1 success), got %d", calls)
	}

	// Verify database checkpoints show attempts and retries
	taskState, _ := mockStore.GetTaskRun(context.Background(), runID, "failing_task")
	if taskState == nil || taskState.Status != "completed" || taskState.AttemptCount != 3 {
		t.Errorf("task state checkpoints incorrect: %+v", taskState)
	}
}


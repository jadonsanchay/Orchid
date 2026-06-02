// Package orchestrator implements the core scheduler, worker, and state transitions
// for executing multi-step workflows.
package orchestrator

import (
	"context"
	"errors"
	"fmt"
	"log"
	"sync"
	"time"

	"orchid/internal/domain"
	"orchid/internal/queue"
	"orchid/internal/store"
)

// WorkflowPlan defines an ordered sequence of task names for a workflow pipeline.
type WorkflowPlan struct {
	Steps []string
}

// NextStep returns the task name following currentTask, or empty string if completed.
func (p *WorkflowPlan) NextStep(currentTask string) string {
	if currentTask == "" {
		if len(p.Steps) > 0 {
			return p.Steps[0]
		}
		return ""
	}
	for i, name := range p.Steps {
		if name == currentTask {
			if i+1 < len(p.Steps) {
				return p.Steps[i+1]
			}
			return ""
		}
	}
	return ""
}

// RetryPolicy defines how many times a task can fail and the duration to wait between retries.
type RetryPolicy struct {
	MaxAttempts int
	Backoff     time.Duration
}

// Engine manages the scheduling and worker execution of registered workflows.
type Engine struct {
	store         store.WorkflowStore
	queue         queue.Queue
	registry      *domain.Registry
	plans         map[string]WorkflowPlan
	retryPolicies map[string]RetryPolicy
}

// NewEngine constructs a new Engine instance.
func NewEngine(store store.WorkflowStore, queue queue.Queue, registry *domain.Registry) *Engine {
	// Initialize default plans and retry policies
	plans := make(map[string]WorkflowPlan)
	retryPolicies := make(map[string]RetryPolicy)
	return &Engine{
		store:         store,
		queue:         queue,
		registry:      registry,
		plans:         plans,
		retryPolicies: retryPolicies,
	}
}

// RegisterPlan registers a linear workflow steps plan for a given workflow type.
func (e *Engine) RegisterPlan(workflowType string, plan WorkflowPlan) {
	e.plans[workflowType] = plan
}

// RegisterRetryPolicy configures a custom retry policy for a task name.
func (e *Engine) RegisterRetryPolicy(taskName string, policy RetryPolicy) {
	e.retryPolicies[taskName] = policy
}

// SubmitRun registers a new run in the database and enqueues its first step, storing any initial input payload.
func (e *Engine) SubmitRun(ctx context.Context, userID string, workflowType string, initialInput map[string]any) (int64, error) {
	plan, exists := e.plans[workflowType]
	if !exists {
		return 0, fmt.Errorf("unsupported workflow type: %s", workflowType)
	}
	if len(plan.Steps) == 0 {
		return 0, fmt.Errorf("workflow plan has no steps configured: %s", workflowType)
	}

	run, err := e.store.CreateRun(ctx, userID, workflowType)
	if err != nil {
		return 0, err
	}

	firstStep := plan.Steps[0]
	_, err = e.store.CreateTaskRun(ctx, run.ID, firstStep)
	if err != nil {
		return 0, err
	}

	if len(initialInput) > 0 {
		err = e.store.UpdateTaskRun(ctx, run.ID, firstStep, "pending", 0, "", initialInput)
		if err != nil {
			return 0, err
		}
	}

	payload := queue.TaskPayload{
		RunID:    run.ID,
		TaskName: firstStep,
		Attempt:  1,
	}
	err = e.queue.Enqueue(ctx, payload)
	if err != nil {
		return 0, err
	}

	log.Printf("[Engine] Workflow Run %d submitted successfully with first task '%s'", run.ID, firstStep)
	return run.ID, nil
}

// Start spawns worker goroutines to process task queue messages.
// It blocks until the context is cancelled and waits for active workers to exit.
func (e *Engine) Start(ctx context.Context, numWorkers int) {
	var wg sync.WaitGroup
	log.Printf("[Engine] Starting workflow engine with %d workers...", numWorkers)

	// 1. Start scheduled task queue scheduler if running on Redis
	if rq, ok := e.queue.(*queue.RedisQueue); ok {
		wg.Add(1)
		go func() {
			defer wg.Done()
			log.Println("[Engine] Scheduled ZSET queue poller started.")
			rq.StartScheduler(ctx)
			log.Println("[Engine] Scheduled ZSET queue poller stopped.")
		}()
	}

	// 2. Start workers
	for i := 1; i <= numWorkers; i++ {
		wg.Add(1)
		go func(workerID int) {
			defer wg.Done()
			log.Printf("[Engine] Worker %d starting...", workerID)
			e.workerLoop(ctx, workerID)
			log.Printf("[Engine] Worker %d stopped.", workerID)
		}(i)
	}

	// 3. Run Crash Recovery Scanner
	log.Println("[Engine] Running crash recovery scanner...")
	incomplete, err := e.store.GetIncompleteTasks(ctx)
	if err != nil {
		log.Printf("[Engine] Crash recovery scanner failed to query DB: %v", err)
	} else {
		for _, taskRun := range incomplete {
			log.Printf("[Engine] Recovering interrupted task '%s' for Run %d. Re-enqueuing (Attempt %d)...", taskRun.TaskName, taskRun.RunID, taskRun.AttemptCount)
			payload := queue.TaskPayload{
				RunID:    taskRun.RunID,
				TaskName: taskRun.TaskName,
				Attempt:  taskRun.AttemptCount,
			}
			_ = e.queue.Enqueue(ctx, payload)
		}
		log.Printf("[Engine] Crash recovery finished. Re-enqueued %d tasks.", len(incomplete))
	}

	<-ctx.Done()
	log.Println("[Engine] Shutdown received. Waiting for workers to terminate...")
	wg.Wait()
	log.Println("[Engine] Engine shut down successfully.")
}

func (e *Engine) workerLoop(ctx context.Context, workerID int) {
	for {
		select {
		case <-ctx.Done():
			return
		default:
		}

		// Blocking pop task (unblocks immediately if context is cancelled)
		payload, err := e.queue.Dequeue(ctx)
		if err != nil {
			if errors.Is(err, context.Canceled) {
				return
			}
			log.Printf("[Engine] Worker %d Dequeue error: %v", workerID, err)
			time.Sleep(200 * time.Millisecond) // Backoff to avoid hot loops
			continue
		}

		e.executeTask(ctx, workerID, payload)
	}
}

func (e *Engine) executeTask(ctx context.Context, workerID int, p *queue.TaskPayload) {
	log.Printf("[Engine] Worker %d processing Run %d, Task %s (Attempt %d)", workerID, p.RunID, p.TaskName, p.Attempt)

	// 1. Fetch run state
	run, err := e.store.GetRun(ctx, p.RunID)
	if err != nil {
		log.Printf("[Engine] Error retrieving Run %d: %v", p.RunID, err)
		return
	}
	if run == nil {
		log.Printf("[Engine] Run %d not found in DB", p.RunID)
		return
	}

	// Skip if run is already in terminal state
	if run.Status == "completed" || run.Status == "failed" {
		log.Printf("[Engine] Run %d is already '%s', skipping task '%s'", p.RunID, run.Status, p.TaskName)
		return
	}

	// 2. Fetch/Create task execution step record
	taskRun, err := e.store.GetTaskRun(ctx, p.RunID, p.TaskName)
	if err != nil {
		log.Printf("[Engine] Error retrieving TaskRun %d:%s: %v", p.RunID, p.TaskName, err)
		return
	}
	if taskRun == nil {
		taskRun, err = e.store.CreateTaskRun(ctx, p.RunID, p.TaskName)
		if err != nil {
			log.Printf("[Engine] Error creating TaskRun %d:%s: %v", p.RunID, p.TaskName, err)
			return
		}
	}

	if taskRun.Status == "completed" {
		log.Printf("[Engine] Task %s for Run %d already completed, skipping", p.TaskName, p.RunID)
		return
	}

	// Update active step in run
	_ = e.store.UpdateRunStatus(ctx, p.RunID, "running", p.TaskName)

	// Increment attempt and mark task as running in DB (preserving existing task run output/inputs)
	err = e.store.UpdateTaskRun(ctx, p.RunID, p.TaskName, "running", p.Attempt, "", taskRun.Output)
	if err != nil {
		log.Printf("[Engine] Error marking TaskRun running: %v", err)
		return
	}

	// 3. Resolve task implementation
	taskImpl := e.registry.Get(p.TaskName)
	if taskImpl == nil {
		errMsg := fmt.Sprintf("unregistered task name: %s", p.TaskName)
		_ = e.store.UpdateTaskRun(ctx, p.RunID, p.TaskName, "failed", p.Attempt, errMsg, nil)
		_ = e.store.UpdateRunStatus(ctx, p.RunID, "failed", p.TaskName)
		log.Printf("[Engine] Task %s execution failed: %s", p.TaskName, errMsg)
		return
	}

	// 4. Resolve task input data (merge outputs of preceding steps for linear chaining)
	inputData := make(map[string]any)
	plan := e.plans[run.WorkflowType]
	precedingStep := ""
	for i, name := range plan.Steps {
		if name == p.TaskName && i > 0 {
			precedingStep = plan.Steps[i-1]
			break
		}
	}

	if precedingStep != "" {
		precTaskRun, err := e.store.GetTaskRun(ctx, p.RunID, precedingStep)
		if err == nil && precTaskRun != nil && precTaskRun.Output != nil {
			for k, v := range precTaskRun.Output {
				inputData[k] = v
			}
		}
	}

	// Merge current task's output if pre-seeded (e.g. initial run inputs)
	if taskRun != nil && taskRun.Output != nil {
		for k, v := range taskRun.Output {
			inputData[k] = v
		}
	}

	taskInput := domain.TaskInput{
		RunID:  p.RunID,
		UserID: run.UserID,
		Data:   inputData,
	}

	// 5. Execute task logic
	taskOutput, err := taskImpl.Execute(ctx, taskInput)
	if err != nil {
		e.handleFailure(ctx, p, err)
		return
	}

	// 6. Checkpoint task success state
	err = e.store.UpdateTaskRun(ctx, p.RunID, p.TaskName, "completed", p.Attempt, "", taskOutput.Data)
	if err != nil {
		log.Printf("[Engine] Error checkpointing task success: %v", err)
		return
	}

	// 7. Schedule next step or terminate pipeline
	next := plan.NextStep(p.TaskName)
	if next == "" {
		_ = e.store.UpdateRunStatus(ctx, p.RunID, "completed", p.TaskName)
		log.Printf("[Engine] Workflow Run %d completed successfully!", p.RunID)
	} else {
		_, err = e.store.CreateTaskRun(ctx, p.RunID, next)
		if err != nil {
			log.Printf("[Engine] Error creating next step TaskRun: %v", err)
			return
		}

		nextPayload := queue.TaskPayload{
			RunID:    p.RunID,
			TaskName: next,
			Attempt:  1,
		}
		err = e.queue.Enqueue(ctx, nextPayload)
		if err != nil {
			log.Printf("[Engine] Error enqueuing next task: %v", err)
			return
		}
		log.Printf("[Engine] Scheduled next step '%s' for Run %d", next, p.RunID)
	}
}

// handleFailure handles task execution errors by scheduling retries or marking the run failed.
func (e *Engine) handleFailure(ctx context.Context, p *queue.TaskPayload, taskErr error) {
	attempt := p.Attempt
	policy, ok := e.retryPolicies[p.TaskName]
	if !ok {
		// Default fallback retry policy: 3 max attempts, 1 second backoff
		policy = RetryPolicy{
			MaxAttempts: 3,
			Backoff:     1 * time.Second,
		}
	}

	if attempt < policy.MaxAttempts {
		nextAttempt := attempt + 1
		// Checkpoint attempt failure as 'retrying'
		_ = e.store.UpdateTaskRun(ctx, p.RunID, p.TaskName, "retrying", attempt, taskErr.Error(), nil)

		nextPayload := queue.TaskPayload{
			RunID:    p.RunID,
			TaskName: p.TaskName,
			Attempt:  nextAttempt,
		}

		// Schedule retry payload with backoff
		err := e.queue.EnqueueScheduled(ctx, nextPayload, policy.Backoff)
		if err != nil {
			log.Printf("[Engine] Failed to schedule retry task: %v", err)
			return
		}
		log.Printf("[Engine] Task '%s' for Run %d failed: %v. Retrying (Attempt %d/%d) in %v...", p.TaskName, p.RunID, taskErr, nextAttempt, policy.MaxAttempts, policy.Backoff)
	} else {
		// Out of retries, mark task and overall run failed
		_ = e.store.UpdateTaskRun(ctx, p.RunID, p.TaskName, "failed", attempt, taskErr.Error(), nil)
		_ = e.store.UpdateRunStatus(ctx, p.RunID, "failed", p.TaskName)
		log.Printf("[Engine] Task '%s' for Run %d failed permanently after %d attempts: %v", p.TaskName, p.RunID, attempt, taskErr)
	}
}

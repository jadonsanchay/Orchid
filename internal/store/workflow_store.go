package store

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
)

// DBExecutor represents the pgx methods required for database queries.
type DBExecutor interface {
	Exec(ctx context.Context, sql string, arguments ...any) (pgconn.CommandTag, error)
	Query(ctx context.Context, sql string, args ...any) (pgx.Rows, error)
	QueryRow(ctx context.Context, sql string, args ...any) pgx.Row
}

// WorkflowRun represents a single orchestration pipeline execution in the database.
type WorkflowRun struct {
	ID           int64     `json:"id"`
	UserID       string    `json:"user_id"`
	WorkflowType string    `json:"workflow_type"`
	Status       string    `json:"status"`
	CurrentStep  string    `json:"current_step"`
	CreatedAt    time.Time `json:"created_at"`
	UpdatedAt    time.Time `json:"updated_at"`
}

// TaskRun represents a single task execution step within a workflow run.
type TaskRun struct {
	ID           int64          `json:"id"`
	RunID        int64          `json:"run_id"`
	TaskName     string         `json:"task_name"`
	Status       string         `json:"status"`
	AttemptCount int            `json:"attempt_count"`
	LastError    *string        `json:"last_error"`
	Output       map[string]any `json:"output"`
	UpdatedAt    time.Time      `json:"updated_at"`
}

// WorkflowStore defines the persistence interface for workflow states.
type WorkflowStore interface {
	CreateRun(ctx context.Context, userID string, workflowType string) (*WorkflowRun, error)
	GetRun(ctx context.Context, runID int64) (*WorkflowRun, error)
	UpdateRunStatus(ctx context.Context, runID int64, status string, currentStep string) error
	CreateTaskRun(ctx context.Context, runID int64, taskName string) (*TaskRun, error)
	GetTaskRun(ctx context.Context, runID int64, taskName string) (*TaskRun, error)
	UpdateTaskRun(ctx context.Context, runID int64, taskName string, status string, attemptCount int, lastError string, output map[string]any) error
	ListRuns(ctx context.Context) ([]WorkflowRun, error)
	GetIncompleteTasks(ctx context.Context) ([]TaskRun, error)
	GetTaskRunsForRun(ctx context.Context, runID int64) ([]TaskRun, error)
}

// PostgresWorkflowStore implements WorkflowStore backed by PostgreSQL.
type PostgresWorkflowStore struct {
	db DBExecutor
}

// NewPostgresWorkflowStore creates a new PostgresWorkflowStore.
func NewPostgresWorkflowStore(db DBExecutor) *PostgresWorkflowStore {
	return &PostgresWorkflowStore{db: db}
}

// CreateRun inserts a new workflow run with status 'pending' in the database.
func (s *PostgresWorkflowStore) CreateRun(ctx context.Context, userID string, workflowType string) (*WorkflowRun, error) {
	query := `
		INSERT INTO workflow_runs (user_id, workflow_type, status)
		VALUES ($1, $2, 'pending')
		RETURNING id, user_id, workflow_type, status, current_step, created_at, updated_at
	`
	var run WorkflowRun
	var currentStep sql.NullString
	err := s.db.QueryRow(ctx, query, userID, workflowType).Scan(
		&run.ID, &run.UserID, &run.WorkflowType, &run.Status, &currentStep, &run.CreatedAt, &run.UpdatedAt,
	)
	if err != nil {
		return nil, fmt.Errorf("failed to create workflow run: %w", err)
	}
	if currentStep.Valid {
		run.CurrentStep = currentStep.String
	}
	return &run, nil
}

// GetRun retrieves a workflow run by its ID.
func (s *PostgresWorkflowStore) GetRun(ctx context.Context, runID int64) (*WorkflowRun, error) {
	query := `
		SELECT id, user_id, workflow_type, status, current_step, created_at, updated_at
		FROM workflow_runs
		WHERE id = $1
	`
	var run WorkflowRun
	var currentStep sql.NullString
	err := s.db.QueryRow(ctx, query, runID).Scan(
		&run.ID, &run.UserID, &run.WorkflowType, &run.Status, &currentStep, &run.CreatedAt, &run.UpdatedAt,
	)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, nil // Return nil if run not found
		}
		return nil, fmt.Errorf("failed to get workflow run: %w", err)
	}
	if currentStep.Valid {
		run.CurrentStep = currentStep.String
	}
	return &run, nil
}

// UpdateRunStatus updates the active step and overall execution status of a run.
func (s *PostgresWorkflowStore) UpdateRunStatus(ctx context.Context, runID int64, status string, currentStep string) error {
	query := `
		UPDATE workflow_runs
		SET status = $1, current_step = $2, updated_at = CURRENT_TIMESTAMP
		WHERE id = $3
	`
	var stepVal *string
	if currentStep != "" {
		stepVal = &currentStep
	}
	_, err := s.db.Exec(ctx, query, status, stepVal, runID)
	if err != nil {
		return fmt.Errorf("failed to update workflow run status: %w", err)
	}
	return nil
}

// CreateTaskRun initialises a step execution record for a given run.
func (s *PostgresWorkflowStore) CreateTaskRun(ctx context.Context, runID int64, taskName string) (*TaskRun, error) {
	query := `
		INSERT INTO task_runs (run_id, task_name, status, attempt_count)
		VALUES ($1, $2, 'pending', 0)
		RETURNING id, run_id, task_name, status, attempt_count, last_error, output, updated_at
	`
	var task TaskRun
	err := s.db.QueryRow(ctx, query, runID, taskName).Scan(
		&task.ID, &task.RunID, &task.TaskName, &task.Status, &task.AttemptCount, &task.LastError, &task.Output, &task.UpdatedAt,
	)
	if err != nil {
		return nil, fmt.Errorf("failed to create task run: %w", err)
	}
	return &task, nil
}

// GetTaskRun fetches an active step execution record.
func (s *PostgresWorkflowStore) GetTaskRun(ctx context.Context, runID int64, taskName string) (*TaskRun, error) {
	query := `
		SELECT id, run_id, task_name, status, attempt_count, last_error, output, updated_at
		FROM task_runs
		WHERE run_id = $1 AND task_name = $2
	`
	var task TaskRun
	err := s.db.QueryRow(ctx, query, runID, taskName).Scan(
		&task.ID, &task.RunID, &task.TaskName, &task.Status, &task.AttemptCount, &task.LastError, &task.Output, &task.UpdatedAt,
	)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, nil
		}
		return nil, fmt.Errorf("failed to get task run: %w", err)
	}
	return &task, nil
}

// UpdateTaskRun checkpoints the progress of a specific task execution step.
func (s *PostgresWorkflowStore) UpdateTaskRun(ctx context.Context, runID int64, taskName string, status string, attemptCount int, lastError string, output map[string]any) error {
	query := `
		UPDATE task_runs
		SET status = $1, attempt_count = $2, last_error = $3, output = $4, updated_at = CURRENT_TIMESTAMP
		WHERE run_id = $5 AND task_name = $6
	`
	var errStr *string
	if lastError != "" {
		errStr = &lastError
	}
	_, err := s.db.Exec(ctx, query, status, attemptCount, errStr, output, runID, taskName)
	if err != nil {
		return fmt.Errorf("failed to update task run status: %w", err)
	}
	return nil
}

// ListRuns lists all recent workflow executions.
func (s *PostgresWorkflowStore) ListRuns(ctx context.Context) ([]WorkflowRun, error) {
	query := `
		SELECT id, user_id, workflow_type, status, current_step, created_at, updated_at
		FROM workflow_runs
		ORDER BY created_at DESC
		LIMIT 50
	`
	rows, err := s.db.Query(ctx, query)
	if err != nil {
		return nil, fmt.Errorf("failed to list workflow runs: %w", err)
	}
	defer rows.Close()

	var runs []WorkflowRun
	for rows.Next() {
		var run WorkflowRun
		var currentStep sql.NullString
		err := rows.Scan(&run.ID, &run.UserID, &run.WorkflowType, &run.Status, &currentStep, &run.CreatedAt, &run.UpdatedAt)
		if err != nil {
			return nil, fmt.Errorf("failed to scan workflow run: %w", err)
		}
		if currentStep.Valid {
			run.CurrentStep = currentStep.String
		}
		runs = append(runs, run)
	}
	return runs, nil
}

// GetIncompleteTasks finds and returns all task runs that are still in 'running' or 'pending' state
// belonging to parent workflow runs that are currently marked 'running'.
func (s *PostgresWorkflowStore) GetIncompleteTasks(ctx context.Context) ([]TaskRun, error) {
	query := `
		SELECT t.id, t.run_id, t.task_name, t.status, t.attempt_count, t.last_error, t.output, t.updated_at
		FROM task_runs t
		JOIN workflow_runs w ON t.run_id = w.id
		WHERE w.status = 'running' AND t.status IN ('running', 'pending')
	`
	rows, err := s.db.Query(ctx, query)
	if err != nil {
		return nil, fmt.Errorf("failed to query incomplete tasks: %w", err)
	}
	defer rows.Close()

	var tasks []TaskRun
	for rows.Next() {
		var task TaskRun
		err := rows.Scan(
			&task.ID, &task.RunID, &task.TaskName, &task.Status, &task.AttemptCount, &task.LastError, &task.Output, &task.UpdatedAt,
		)
		if err != nil {
			return nil, fmt.Errorf("failed to scan task run row: %w", err)
		}
		tasks = append(tasks, task)
	}

	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("error reading incomplete task rows: %w", err)
	}

	return tasks, nil
}

// GetTaskRunsForRun retrieves all task run executions for a given workflow run.
func (s *PostgresWorkflowStore) GetTaskRunsForRun(ctx context.Context, runID int64) ([]TaskRun, error) {
	query := `
		SELECT id, run_id, task_name, status, attempt_count, last_error, output, updated_at
		FROM task_runs
		WHERE run_id = $1
		ORDER BY id ASC
	`
	rows, err := s.db.Query(ctx, query, runID)
	if err != nil {
		return nil, fmt.Errorf("failed to query task runs for run: %w", err)
	}
	defer rows.Close()

	var tasks []TaskRun
	for rows.Next() {
		var task TaskRun
		err := rows.Scan(
			&task.ID, &task.RunID, &task.TaskName, &task.Status, &task.AttemptCount, &task.LastError, &task.Output, &task.UpdatedAt,
		)
		if err != nil {
			return nil, fmt.Errorf("failed to scan task run row: %w", err)
		}
		tasks = append(tasks, task)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("error reading task run rows: %w", err)
	}
	return tasks, nil
}


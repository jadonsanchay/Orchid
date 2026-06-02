package store

import (
	"context"
	"os"
	"testing"

	"orchid/pkg/utils"

	"github.com/jackc/pgx/v5"
)

func TestPostgresWorkflowStore_Integration(t *testing.T) {
	utils.LoadEnv() // Load .env file configurations locally
	dbURL := os.Getenv("TEST_DATABASE_URL")
	if dbURL == "" {
		t.Skip("Skipping workflow store integration test. Set TEST_DATABASE_URL to run.")
	}

	ctx := context.Background()
	conn, err := pgx.Connect(ctx, dbURL)
	if err != nil {
		t.Fatalf("failed to connect to test database: %v", err)
	}
	defer conn.Close(ctx)

	// Dynamically create schema for testing
	_, err = conn.Exec(ctx, `
		CREATE TABLE IF NOT EXISTS workflow_runs (
			id            BIGSERIAL PRIMARY KEY,
			user_id       TEXT NOT NULL,
			workflow_type TEXT NOT NULL,
			status        TEXT NOT NULL DEFAULT 'pending',
			current_step  TEXT,
			created_at    TIMESTAMP WITH TIME ZONE NOT NULL DEFAULT CURRENT_TIMESTAMP,
			updated_at    TIMESTAMP WITH TIME ZONE NOT NULL DEFAULT CURRENT_TIMESTAMP
		);
		CREATE TABLE IF NOT EXISTS task_runs (
			id            BIGSERIAL PRIMARY KEY,
			run_id        BIGINT NOT NULL REFERENCES workflow_runs(id) ON DELETE CASCADE,
			task_name     TEXT NOT NULL,
			status        TEXT NOT NULL DEFAULT 'pending',
			attempt_count INT NOT NULL DEFAULT 0,
			last_error    TEXT,
			output        JSONB,
			updated_at    TIMESTAMP WITH TIME ZONE NOT NULL DEFAULT CURRENT_TIMESTAMP,
			UNIQUE(run_id, task_name)
		);
	`)
	if err != nil {
		t.Fatalf("failed to set up test schema: %v", err)
	}

	// Clean up after run
	defer func() {
		_, _ = conn.Exec(ctx, "TRUNCATE TABLE workflow_runs CASCADE")
	}()

	store := NewPostgresWorkflowStore(conn)

	// 1. Test CreateRun
	run, err := store.CreateRun(ctx, "user-123", "job-hunt-pipeline")
	if err != nil {
		t.Fatalf("CreateRun failed: %v", err)
	}
	if run.UserID != "user-123" || run.Status != "pending" || run.WorkflowType != "job-hunt-pipeline" {
		t.Errorf("unexpected run fields: %+v", run)
	}

	// 2. Test GetRun
	fetchedRun, err := store.GetRun(ctx, run.ID)
	if err != nil {
		t.Fatalf("GetRun failed: %v", err)
	}
	if fetchedRun == nil || fetchedRun.ID != run.ID {
		t.Errorf("expected to fetch run %d, got %+v", run.ID, fetchedRun)
	}

	// 3. Test UpdateRunStatus
	err = store.UpdateRunStatus(ctx, run.ID, "running", "ingest")
	if err != nil {
		t.Fatalf("UpdateRunStatus failed: %v", err)
	}
	fetchedRun, _ = store.GetRun(ctx, run.ID)
	if fetchedRun.Status != "running" || fetchedRun.CurrentStep != "ingest" {
		t.Errorf("expected status 'running' and step 'ingest', got status '%s' and step '%s'", fetchedRun.Status, fetchedRun.CurrentStep)
	}

	// 4. Test CreateTaskRun
	taskRun, err := store.CreateTaskRun(ctx, run.ID, "ingest")
	if err != nil {
		t.Fatalf("CreateTaskRun failed: %v", err)
	}
	if taskRun.TaskName != "ingest" || taskRun.Status != "pending" || taskRun.AttemptCount != 0 {
		t.Errorf("unexpected task fields: %+v", taskRun)
	}

	// 5. Test UpdateTaskRun (with simulated output maps)
	outputMap := map[string]any{
		"jobs_count": float64(10), // JSON floats
		"status":     "success",
	}
	err = store.UpdateTaskRun(ctx, run.ID, "ingest", "completed", 1, "", outputMap)
	if err != nil {
		t.Fatalf("UpdateTaskRun failed: %v", err)
	}

	fetchedTask, err := store.GetTaskRun(ctx, run.ID, "ingest")
	if err != nil {
		t.Fatalf("GetTaskRun failed: %v", err)
	}
	if fetchedTask.Status != "completed" || fetchedTask.AttemptCount != 1 || fetchedTask.LastError != nil {
		t.Errorf("unexpected task values: %+v", fetchedTask)
	}
	if fetchedTask.Output["jobs_count"] != float64(10) {
		t.Errorf("expected output job count 10, got %v", fetchedTask.Output["jobs_count"])
	}

	// 6. Test ListRecentRuns
	runs, err := store.ListRuns(ctx)
	if err != nil {
		t.Fatalf("ListRuns failed: %v", err)
	}
	if len(runs) == 0 {
		t.Error("expected at least 1 run in list")
	}

	// 7. Test GetIncompleteTasks
	// Currently run is 'running' (from step 3).
	// We create a pending task 'sleep'
	_, err = store.CreateTaskRun(ctx, run.ID, "sleep")
	if err != nil {
		t.Fatalf("CreateTaskRun failed for incomplete check: %v", err)
	}

	incomplete, err := store.GetIncompleteTasks(ctx)
	if err != nil {
		t.Fatalf("GetIncompleteTasks failed: %v", err)
	}
	
	foundSleep := false
	for _, tRun := range incomplete {
		if tRun.RunID == run.ID && tRun.TaskName == "sleep" {
			foundSleep = true
			break
		}
	}
	if !foundSleep {
		t.Errorf("GetIncompleteTasks failed to return the pending 'sleep' task for running run")
	}
}

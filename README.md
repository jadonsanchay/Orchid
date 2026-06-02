# Orchid — Fault-Tolerant Workflow Orchestration Engine

Orchid is a fault-tolerant workflow orchestration engine built in Go, PostgreSQL (with `pgvector`), and Redis. It is designed to coordinate job-search pipelines (`ingest ➔ match ➔ tailor ➔ apply`) concurrently, saving state checkpoints to survive process crashes, and retrying transient failures automatically.

---

## Table of Contents
- [System Architecture](#system-architecture)
- [Workflow Orchestrator Engine](#workflow-orchestrator-engine)
- [Crash Recovery & Idempotency](#crash-recovery--idempotency)
- [Retry Policies & Redis Backoff](#retry-policies--redis-backoff)
- [Database Schema & Migrations](#database-schema--migrations)
- [Running Orchid Locally](#running-orchid-locally)
- [REST API Endpoints](#rest-api-endpoints)
- [Visual Progress Dashboard](#visual-progress-dashboard)
- [Running the Test Suite](#running-the-test-suite)

---

## System Architecture

Orchid decouples embedding generation and vector search from orchestration using a pluggable worker pool executing Redis-backed queues.

```mermaid
graph TD
    User[HTTP Client / Dashboard] -->|POST /api/runs| Server[HTTP Server & Engine API]
    Server -->|Create Run & Task State| DB[(PostgreSQL + pgvector)]
    Server -->|Enqueue First Step| RedisQueue[Redis Task Queue LIST]
    
    subgraph Worker Pool
        Worker1[Worker Goroutine 1]
        Worker2[Worker Goroutine 2]
    end
    
    RedisQueue -->|Dequeue BLPOP| Worker1
    Worker1 -->|Execute domain.Task| IngestTask[Ingest Task]
    Worker1 -->|Execute domain.Task| MatchTask[Match Task]
    Worker1 -->|Execute domain.Task| TailorTask[Tailor Task]
    Worker1 -->|Execute domain.Task| ApplyTask[Apply Task]
    
    Worker1 -->|Save Checkpoint Output| DB
    Worker1 -->|Enqueue Next Step| RedisQueue
    Worker1 -->|Delay Retry ZADD| RedisZSet[Redis Scheduled ZSET]
    
    RedisScheduler[ZSET Poller Loop] -->|Move Due Tasks| RedisQueue
```

---

## Workflow Orchestrator Engine

The core engine is located in `internal/orchestrator/orchestrator.go`. It drives multi-step linear pipelines concurrently.
- **Workflow Plan**: Defined as a sequence of task names (e.g. `["ingest", "match", "tailor", "apply"]`).
- **State Checkpointing**: The engine updates the database status of the workflow run and task steps to `running`, `completed`, or `failed` at the start and completion of each task.
- **Data Chaining**: Tasks consume preceding outputs and propagate them downstream using a generic **Passthrough Data Pattern**.

---

## Crash Recovery & Idempotency

Orchid is built to survive process restarts mid-execution:
1. **Startup Recovery Scanner**: On boot, the engine scans the database for active tasks in a `running` or `pending` state belonging to runs that are marked `running`.
2. **Re-enqueuing**: Unfinished tasks are automatically re-enqueued into the task queue, resuming execution from the last successful checkpoint instead of starting the entire pipeline over.
3. **Idempotency**: Task implementations are designed to be safe to re-run.

---

## Retry Policies & Redis Backoff

Transient errors (like network timeouts or API limits) are handled gracefully:
- **Retry Policies**: Tasks can be configured with a `MaxAttempts` limit and a `Backoff` duration.
- **ZSET Delayed Scheduling**: On task failure, if attempts remain, the task is pushed to a Redis Sorted Set (`orchid:task_queue:scheduled`) with a score set to the Unix epoch of its next retry time.
- **ZSET Poller**: A background scheduler ticker checks the Sorted Set every 500ms and moves ready tasks back into the active `BLPOP` queue.

---

## Database Schema & Migrations

The Postgres schema (managed in `db/schema.sql`) records runs and tasks:

```sql
CREATE TABLE workflow_runs (
    id BIGSERIAL PRIMARY KEY,
    user_id TEXT NOT NULL,
    workflow_type TEXT NOT NULL,
    status TEXT NOT NULL DEFAULT 'pending', -- pending|running|completed|failed
    current_step TEXT,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE TABLE task_runs (
    id BIGSERIAL PRIMARY KEY,
    run_id BIGINT NOT NULL REFERENCES workflow_runs(id) ON DELETE CASCADE,
    task_name TEXT NOT NULL,
    status TEXT NOT NULL DEFAULT 'pending', -- pending|running|completed|failed|retrying
    attempt_count INT NOT NULL DEFAULT 0,
    last_error TEXT,
    output JSONB,
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now()
);
```

---

## Running Orchid Locally

### 1. Spin up Infrastructure (Docker)
Starts local Redis on port `6379` and PostgreSQL (with `pgvector`) on port `5433` (to avoid host conflicts):
```powershell
docker compose up -d
```

### 2. Configure Environment variables
Create a `.env` file in the project root:
```env
SUPABASE_DB_URL=postgresql://orchid:orchid@localhost:5433/orchid
REDIS_URL=localhost:6379
OPENAI_API_KEY=your_key_here
# OR
GEMINI_API_KEY=your_key_here
```

### 3. Run Migrations
Applies tables and extensions to the local database:
```powershell
go run ./cmd/migrate
```

### 4. Start HTTP Server and Workflow Engine
Launches the HTTP web server and starts background orchestration workers:
```powershell
go run ./cmd/server
```

---

## REST API Endpoints

### 1. Submit Workflow Run
- **Endpoint**: `POST /api/runs`
- **Payload**:
  ```json
  {
    "user_id": "user-abc",
    "resume_text": "Experienced software engineer with Go and Redis background."
  }
  ```
- **Returns**: `{"status":"submitted","run_id":1}`

### 2. List Recent Workflow Runs
- **Endpoint**: `GET /api/runs`
- **Returns**: JSON list of workflow executions.

### 3. Fetch Single Run Trace
- **Endpoint**: `GET /api/runs?id=<run_id>`
- **Returns**: Full details of the run along with each task's checkpoints, attempt counts, errors, and output data.

---

## Visual Progress Dashboard

Orchid includes a premium dark-mode dashboard to monitor pipelines in real-time.

- **URL**: Open `http://localhost:8080/` in your web browser.
- **Features**:
  - **Aesthetics**: Glassmorphic panels, ambient gradient glows, custom visual transitions.
  - **Live Step Progress**: Real-time status trackers (pending, spinning loaders, checks, error crosses).
  - **Interactive submissions**: Form to trigger new runs directly from the browser.
  - **Checkpoint inspector**: JSON syntax highlighting for task logs and outputs.

---

## Running the Test Suite

All packages are fully tested with mocks to ensure clean, isolated, and offline-compatible runs.

### Run All Tests
```powershell
go test -v ./...
```
*Includes: registry tests, queue integration tests, engine recovery/retry tests, and end-to-end task chaining logic tests.*

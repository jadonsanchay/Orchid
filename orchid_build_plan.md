# Orchid — Complete Build Plan

**Workflow Orchestration Engine with Intelligent Job Matching**
Stack: Go · PostgreSQL + pgvector · Redis

This is your working checklist. Build top to bottom. Each milestone ends in something you can *run and see*, so you always feel progress. Tick the boxes as you go.

---

## Part A — What you're actually building (and why it's worth it)

### The one-sentence version
A small engine that runs a user's job-hunt pipeline (`find jobs → match them to a resume → tailor the resume → apply`) as a sequence of steps that survive crashes, retry failures, and run for many users at once.

### What "done" looks like
- You hit one API endpoint to start a run.
- The engine works through the steps in the background.
- It saves its progress to Postgres after every step.
- If it crashes, on restart it resumes from the last completed step instead of starting over.
- If a step fails (e.g. an API timeout), it retries with backoff before giving up.
- A small dashboard/CLI shows runs and their live step status.

### The resume bullets you earn (don't write these until they're true)
- Built a fault-tolerant workflow orchestration engine in Go that executes multi-step pipelines as state machines with persistent checkpointing, surviving process restarts mid-execution.
- Designed a pluggable task interface and Redis-backed queue enabling concurrent execution of independent workflows with configurable per-task retry/backoff policies.
- Implemented semantic job-to-resume matching using pgvector cosine-similarity search over embedding vectors.

### Where the "wow" comes from
Most projects are CRUD. This one demonstrates systems thinking: concurrency, failure handling, state recovery, background processing, vector search. That is senior-flavored backend work. The crash-and-resume demo is your portfolio centerpiece.

---

## Part B — Milestones

> **Time:** ~2–3 weeks part-time if Go is new to you. Don't rush; the engine (M4) is where the learning lives.
> **MVP cut-line:** finishing through **M5** = a complete, defensible project. M6+ makes it shine.

### M0 — Skeleton + infrastructure  ·  *~½ day*
Goal: empty project that talks to Postgres and Redis.

- [ ] `mkdir orchid && cd orchid && go mod init github.com/<you>/orchid`
- [ ] Create `docker-compose.yml` (below) and run `docker compose up -d`
- [ ] `go get github.com/jackc/pgx/v5 github.com/redis/go-redis/v9`
- [ ] Write a tiny `main.go` that connects to both and prints "connected"
- [ ] **Done when:** `go run ./cmd/server` prints successful connections to Postgres + Redis.

```yaml
# docker-compose.yml
services:
  postgres:
    image: pgvector/pgvector:pg16
    environment:
      POSTGRES_USER: orchid
      POSTGRES_PASSWORD: orchid
      POSTGRES_DB: orchid
    ports: ["5432:5432"]
    volumes: [pgdata:/var/lib/postgresql/data]
  redis:
    image: redis:7-alpine
    ports: ["6379:6379"]
volumes:
  pgdata:
```

**Folder layout** (create these now, fill them in across milestones):
```
orchid/
├── cmd/
│   ├── server/       # HTTP API entrypoint
│   └── worker/       # worker entrypoint
├── internal/
│   ├── domain/       # core types: Run, Task interface, Status
│   ├── store/        # Postgres repos (pgx)
│   ├── queue/        # Redis queue wrapper
│   ├── orchestrator/ # THE ENGINE (scheduler, executor, retry)
│   ├── tasks/        # concrete steps: ingest, match, tailor, apply
│   └── embed/        # Embedder interface + real impl
├── migrations/
├── docker-compose.yml
└── README.md
```

---

### M1 — Data layer  ·  *~1 day*
Goal: the tables that make the engine "stateful" exist, and Go can read/write them.

- [ ] Write `migrations/001_init.sql` (below) and apply it
- [ ] Build `internal/store` with pgx: `CreateRun`, `GetRun`, `UpdateRunStatus`, `CreateTaskRun`, `UpdateTaskRun`, `ListRuns`
- [ ] Write a seed script that inserts a few fake `jobs`
- [ ] **Done when:** you can create a run + its task rows from Go and read them back.

```sql
-- migrations/001_init.sql
CREATE EXTENSION IF NOT EXISTS vector;   -- note: 'vector', NOT 'pgvector'

CREATE TABLE jobs (
  id          BIGSERIAL PRIMARY KEY,
  title       TEXT NOT NULL,
  company     TEXT,
  description TEXT NOT NULL,
  embedding   VECTOR(1536),              -- match your embedding model's size
  created_at  TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE TABLE workflow_runs (
  id            BIGSERIAL PRIMARY KEY,
  user_id       TEXT NOT NULL,
  workflow_type TEXT NOT NULL,
  status        TEXT NOT NULL DEFAULT 'pending',  -- pending|running|completed|failed
  current_step  TEXT,
  created_at    TIMESTAMPTZ NOT NULL DEFAULT now(),
  updated_at    TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE TABLE task_runs (
  id            BIGSERIAL PRIMARY KEY,
  run_id        BIGINT NOT NULL REFERENCES workflow_runs(id) ON DELETE CASCADE,
  task_name     TEXT NOT NULL,
  status        TEXT NOT NULL DEFAULT 'pending',  -- pending|running|completed|failed
  attempt_count INT  NOT NULL DEFAULT 0,
  last_error    TEXT,
  output        JSONB,
  updated_at    TIMESTAMPTZ NOT NULL DEFAULT now()
);
```
> Skip the vector index until you have data. When you do: `CREATE INDEX ON jobs USING hnsw (embedding vector_cosine_ops);`

---

### M2 — Domain types + task interface  ·  *~½ day*
Goal: the "pluggable" contract every step obeys.

- [ ] Define status constants and the `Task` interface in `internal/domain`
- [ ] Build a `TaskRegistry` (`map[string]Task`)
- [ ] Write two **mock** tasks (`hello`, `sleep`) that just log and return
- [ ] **Done when:** `registry.Get("sleep").Execute(ctx, input)` runs.

```go
// internal/domain/task.go
type TaskInput struct {
    RunID  int64
    UserID string
    Data   map[string]any // output carried from the previous step
}

type TaskOutput struct {
    Data map[string]any
}

type Task interface {
    Name() string
    Execute(ctx context.Context, in TaskInput) (TaskOutput, error)
}
```

---

### M3 — Redis queue wrapper  ·  *~½ day*
Goal: a clean way to push and pull work.

- [ ] `internal/queue` with `Enqueue(payload)` and `Dequeue()` (blocking `BLPOP`)
- [ ] Payload struct: `{ RunID int64, TaskName string, Attempt int }` (JSON)
- [ ] **Done when:** one terminal enqueues, another pops and prints it.

> v1 uses a plain list + `BLPOP`. Note in your README that the production-grade upgrade is `BLMOVE` into a processing list so a task isn't lost if a worker dies after popping. Knowing the limitation is itself interview gold.

---

### M4 — The orchestrator core  ·  *~2–3 days*  ⭐ THE PROJECT
Goal: a worker that drives a pipeline step by step, checkpointing as it goes.

- [ ] Define a **plan**: an ordered list of step names per `workflow_type`, e.g. `["ingest", "match", "tailor", "apply"]`, plus a `nextStep(current)` helper
- [ ] Worker loop in `internal/orchestrator`:
  1. `Dequeue()` a payload
  2. Load the run + task row from Postgres; mark task `running`
  3. `registry.Get(taskName).Execute(...)`
  4. On success: save output to `task_runs`, mark task `completed`, set the run's `current_step` to the next step, enqueue the next task (or mark the run `completed` if none)
  5. On error: hand to the retry logic (M6) — for now just mark `failed`
- [ ] Start a run: API/CLI creates the run row + first task row + enqueues the first task
- [ ] Run **N workers** as goroutines; wire up graceful shutdown with `context.Context`
- [ ] **Done when:** a run with the two mock tasks flows `ingest→…→complete` end-to-end, and two runs process concurrently.

```
// pseudocode for one worker
for {
    p := queue.Dequeue()                  // blocks
    task := registry.Get(p.TaskName)
    store.MarkTaskRunning(p.RunID, p.TaskName)

    out, err := task.Execute(ctx, buildInput(p))
    if err != nil {
        handleFailure(p, err)             // M6
        continue
    }

    store.MarkTaskCompleted(p.RunID, p.TaskName, out)
    next := nextStep(p.TaskName)
    if next == "" {
        store.MarkRunCompleted(p.RunID)
    } else {
        store.SetCurrentStep(p.RunID, next)
        store.CreateTaskRun(p.RunID, next)
        queue.Enqueue(Payload{RunID: p.RunID, TaskName: next})
    }
}
```
> Start with a **linear** pipeline. A straight line *is* a valid DAG. If time allows, generalize `nextStep` to read dependencies so steps can branch — but linear is enough to call it done.

---

### M5 — Crash recovery  ·  *~1 day*  (this is your demo)
Goal: surviving a restart.

- [ ] On worker startup, scan Postgres for runs in `running` whose current task isn't `completed`, and re-enqueue that task
- [ ] Make tasks safe to re-run (idempotent): re-running a step shouldn't double-apply effects
- [ ] **Done when:** you `Ctrl-C` mid-run, restart, and it resumes from the last checkpoint. **Record this.**

---

### M6 — Retry + backoff policies  ·  *~1 day*
Goal: transient failures don't kill the pipeline.

- [ ] `RetryPolicy{ MaxAttempts int, Backoff time.Duration }` per task
- [ ] On failure: if `attempt_count < MaxAttempts`, increment and re-enqueue after the backoff; else mark task + run `failed` with `last_error`
- [ ] Delayed re-enqueue: simplest is a Redis sorted set (`ZADD` with score = unix time when ready) plus a small poller that moves due items into the work list
- [ ] **Done when:** a task you force to fail twice succeeds on attempt 3; a task that always fails ends as `failed` after max attempts.

---

### M7 — Real tasks  ·  *~2–3 days*
Goal: swap mocks for the real pipeline, one at a time. Keep the mocks for tests.

- [ ] `internal/embed`: `Embedder` interface `Embed(ctx, text) ([]float32, error)` + one real impl (OpenAI **or** Gemini) + a fake impl for tests
- [ ] **`ingest`**: load N jobs from a JSON file, embed each description, store the vector (skip real scraping for v1)
- [ ] **`match`**: embed the resume, run the cosine query below, save matched job IDs in the task output
- [ ] **`tailor`**: LLM call to rewrite the resume summary for the top match; save result
- [ ] **`apply`**: mock it — just log "applied to job X"
- [ ] **Done when:** a real run goes `ingest → match → tailor` with real data.

```sql
-- the matching query ( <=> = cosine distance; 1 - distance = similarity )
SELECT id, title, company, 1 - (embedding <=> $1) AS similarity
FROM jobs
WHERE embedding IS NOT NULL
ORDER BY embedding <=> $1 ASC
LIMIT 5;
```
> Pick the embedding model **before** writing the schema — its dimension must match `VECTOR(n)`. OpenAI `text-embedding-3-small` = 1536. Gemini models differ (often 768 or 3072). Changing it later means migrating the column.

---

### M8 — API surface  ·  *~1 day*
Goal: drive and observe the engine over HTTP.

- [ ] `POST /runs` → create + start a run, return its id
- [ ] `GET /runs/:id` → run status + ordered task history
- [ ] `GET /runs` → list recent runs
- [ ] Structured logging with a run id on every line
- [ ] **Done when:** you can start a run and watch its status change via curl.

---

### M9 — Tests + README  ·  *~1 day*
Goal: make it credible.

- [ ] Unit tests on state transitions and retry logic (use the fake store + fake tasks)
- [ ] One integration test: full pipeline with the fake embedder
- [ ] README: architecture diagram, the data flow, your design decisions, "how to run," and an honest "known limitations / next steps" section
- [ ] **Done when:** `go test ./...` passes and a stranger could run the project from your README.

---

### M10 — Demo layer  ·  *~1 day*  (high leverage for portfolio)
Goal: make the invisible engine visible.

- [ ] A single HTML page (or a CLI) that polls `GET /runs/:id` and renders each step's status with color (pending/running/done/failed)
- [ ] Trigger a run, screen-record the steps advancing, force a failure to show a retry, then kill+restart to show recovery
- [ ] **Done when:** you have a 30–60s clip that makes someone go "oh, that's cool" without reading any code.

---

## Part C — Things that will save you pain

**Learn just enough Go first (one focused day):** interfaces, structs + pointers, goroutines, channels, `error` as a return value (no try/catch), and `context.Context`. These five carry the whole project. If you can *read* them, AI-generated code stops being a black box you can't debug.

**Using Antigravity well:** build one milestone per session, not the whole app. Tell it the milestone's "done when," tell it the folder it belongs in, and ask for the plan before code. Read every file it writes — you'll be the one debugging it. When something breaks, paste the exact error, not "it doesn't work."

**Don't build real scrapers.** Seed jobs from JSON. Real LinkedIn/Indeed scraping is a rabbit hole that adds zero to the engine you're being graded on. Mock `apply` the same way.

**Commit per milestone.** Each milestone is a clean, working checkpoint — perfect commit boundaries and a tidy git history that itself looks professional.

---

## Quick status tracker
- [ ] M0 Skeleton + infra
- [ ] M1 Data layer
- [ ] M2 Task interface
- [ ] M3 Queue
- [ ] M4 Orchestrator core ⭐
- [ ] M5 Crash recovery ← MVP complete here
- [ ] M6 Retry policies
- [ ] M7 Real tasks
- [ ] M8 API
- [ ] M9 Tests + README
- [ ] M10 Demo layer

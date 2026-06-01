# Orchid Vector Embedding Layer

This module provides the core Vector Embedding and Similarity Matching Layer for **Orchid**, a high-performance workflow orchestration engine designed for job search automation.

## Table of Contents
- [Architecture Overview](#architecture-overview)
- [Database Schema (Supabase)](#database-schema-supabase)
- [AI Embedding Client Wrapper](#ai-embedding-client-wrapper)
- [Job Matching Repository](#job-matching-repository)
- [Pluggable Task Interface](#pluggable-task-interface)
- [Running the CLI Demo](#running-the-cli-demo)
- [Running Tests](#running-tests)

---

## Architecture Overview

The layer decouples embedding generation (interacting with external LLM APIs) from database storage and query operations (interacting with Supabase PostgreSQL). This design enables both steps to act as standalone, fault-tolerant workflow tasks that can be retried, throttled, and run asynchronously.

```mermaid
graph TD
    A[Text Input: Resume/Job] --> B[Embedding Task]
    B -->|API Request| C[OpenAI / Gemini]
    C -->|Vector Output| B
    B -->|JSON Vector| D[Matching Task]
    D -->|Cosine Similarity Query| E[Supabase DB / pgvector]
    E -->|Sorted Jobs List| D
    D -->|JSON Output| F[Workflow Orchestrator]
```

---

## Database Schema (Supabase)

Supabase PostgreSQL provides the `pgvector` extension natively.
The database schema (`db/schema.sql`) uses a **dimension-less vector column** (`embedding VECTOR`). This allows you to store vectors of different sizes (e.g. OpenAI's 1536-dim or Gemini's 768-dim) without modifying the table structure.

To create the table, run the SQL script in your Supabase SQL Editor:
```sql
-- Enable Extensions
CREATE EXTENSION IF NOT EXISTS "uuid-ossp";
CREATE EXTENSION IF NOT EXISTS vector;

-- Create Jobs Table
CREATE TABLE IF NOT EXISTS jobs (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    title VARCHAR(255) NOT NULL,
    company VARCHAR(255) NOT NULL,
    location VARCHAR(255),
    description TEXT NOT NULL,
    embedding VECTOR, -- Dimension-less vector
    created_at TIMESTAMP WITH TIME ZONE DEFAULT CURRENT_TIMESTAMP,
    updated_at TIMESTAMP WITH TIME ZONE DEFAULT CURRENT_TIMESTAMP
);
```

### Performance Optimization (Indexing)
For production environments with large datasets, you can speed up nearest-neighbor queries by creating an **HNSW (Hierarchical Navigable Small World)** index.

```sql
-- Run this if you are using OpenAI (1536 dimensions):
CREATE INDEX ON jobs USING hnsw (embedding vector_cosine_ops);

-- Run this if you are using Gemini (768 dimensions):
CREATE INDEX ON jobs USING hnsw (embedding vector_cosine_ops);
```

---

## AI Embedding Client Wrapper

The module abstracts AI model interaction under the `embedding.Client` interface:
```go
type Client interface {
	GetEmbedding(ctx context.Context, text string) ([]float32, error)
	Provider() Provider
	Dimension() int
}
```

### Factory and Configuration
A provider factory constructs clients based on environment keys or configuration parameters:
- **OpenAI**: Uses `text-embedding-3-small` (1536 dimensions) by default. Requires `OPENAI_API_KEY`.
- **Google Gemini**: Uses `text-embedding-004` (768 dimensions) by default. Requires `GEMINI_API_KEY`.

---

## Job Matching Repository

The repository performs cosine-similarity matching against job records using the `<=>` operator (Cosine Distance).
Because **Cosine Similarity = 1 - Cosine Distance**, the matching query filters on a similarity score > 0.75 and returns top matches ordered by closest similarity first:

```sql
SELECT id, title, company, location, description, 1 - (embedding <=> $1) as similarity
FROM jobs
WHERE 1 - (embedding <=> $1) > 0.75
ORDER BY embedding <=> $1
LIMIT $2;
```

---

## Pluggable Task Interface

To run inside Orchid's workflow worker nodes, all steps implement the generic `Task` interface:

```go
type Task interface {
	Name() string
	Execute(ctx context.Context, input []byte) ([]byte, error)
}
```

### 1. `generate-embedding` Task
- **Input JSON**:
  ```json
  { "text": "Resume contents..." }
  ```
- **Output JSON**:
  ```json
  { "embedding": [0.012, -0.045, ...], "provider": "openai" }
  ```

### 2. `match-jobs` Task
- **Input JSON** (Accepts either pre-calculated vector embedding or raw text):
  ```json
  {
    "embedding": [0.012, -0.045, ...],
    "limit": 5,
    "min_similarity": 0.75
  }
  ```
- **Output JSON**:
  ```json
  {
    "matches": [
      {
        "id": "2d174668-...",
        "title": "Backend Go Engineer",
        "company": "DemoCorp",
        "location": "Remote",
        "description": "...",
        "similarity": 0.8845
      }
    ]
  }
  ```

---

## Running the CLI Demo

A comprehensive CLI utility is provided at `cmd/demo/main.go`. It can run using a real Supabase database or in local **offline Mock DB mode**.

### Set API keys
```powershell
# Set API keys
$env:OPENAI_API_KEY="your-openai-api-key"
# OR
$env:GEMINI_API_KEY="your-gemini-api-key"
```

### Option A: Local Mock DB Mode (Default)
Runs entirely locally using deterministic mocked embeddings and databases:
```bash
go run cmd/demo/main.go --provider openai --text "Seeking a Go developer"
```

### Option B: Real Supabase Mode
Connects to Supabase, applies schema, seeds mock job descriptions with real AI embeddings, and queries matches:
```bash
go run cmd/demo/main.go --provider openai --db "postgresql://postgres:[password]@db.[ref].supabase.co:5432/postgres" --seed --text "Go backend and SQL expert"
```

---

## Running Tests

### Unit Tests
Runs the complete test suite utilizing mocks (does not require external API or DB access):
```bash
go test -v ./pkg/task/...
```

### Integration Tests (PostgreSQL)
To test pgvector querying against a real PostgreSQL/Supabase database:
```bash
$env:TEST_DATABASE_URL="postgresql://postgres:[password]@db.[ref].supabase.co:5432/postgres"
go test -v ./pkg/repository/...
```
*Note: The integration test creates an ephemeral schema on the test database, runs vector matching calculations, and performs clean-ups automatically.*

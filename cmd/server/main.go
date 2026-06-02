// Command server provides an HTTP API to trigger and monitor job-search orchestration runs.
package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"strconv"

	"orchid/internal/domain"
	"orchid/internal/orchestrator"
	"orchid/internal/queue"
	"orchid/internal/store"
	"orchid/internal/tasks"
	"orchid/pkg/embedding"
	"orchid/pkg/repository"
	"orchid/pkg/task"
	"orchid/pkg/utils"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/redis/go-redis/v9"
)

// Server holds the dependencies for our HTTP server, including database connection,
// embedding client, and the background workflow engine/store.
type Server struct {
	client  embedding.Client
	repo    repository.JobRepository
	pool    *pgxpool.Pool
	wfStore store.WorkflowStore
	engine  *orchestrator.Engine
}

// RunSubmissionRequest defines the payload for creating a new workflow run.
type RunSubmissionRequest struct {
	UserID     string `json:"user_id"`
	ResumeText string `json:"resume_text"`
}

func main() {
	utils.LoadEnv() // Load local .env variables

	port := flag.Int("port", 8080, "Port to run the HTTP server on")
	provider := flag.String("provider", "openai", "AI Provider: 'openai' or 'gemini'")
	apiKey := flag.String("apikey", "", "API key (optional)")
	dbURL := flag.String("db", "", "Supabase Postgres Connection URL")
	redisURLFlag := flag.String("redis", "", "Redis server address")
	flag.Parse()

	ctx := context.Background()

	// 1. Initialize Embedding Client
	embConfig := embedding.Config{
		Provider: embedding.Provider(*provider),
		APIKey:   *apiKey,
	}
	client, err := embedding.NewClient(embConfig)
	if err != nil {
		log.Printf("Warning: Failed to initialize embedding client: %v. Running in Mock Embedding mode.\n", err)
	}

	// 2. Initialize PostgreSQL Pool
	url := *dbURL
	if url == "" {
		url = os.Getenv("SUPABASE_DB_URL")
	}

	var repo repository.JobRepository
	var pool *pgxpool.Pool

	if url != "" {
		escapedURL := utils.EscapeConnectionURI(url)
		log.Println("Connecting to PostgreSQL pool...")
		pool, err = pgxpool.New(ctx, escapedURL)
		if err != nil {
			log.Fatalf("Unable to connect to database pool: %v", err)
		}
		defer pool.Close()
		repo = repository.NewPostgresJobRepository(pool)
		log.Println("Database connection pool established.")
	} else {
		log.Println("Warning: No database connection URL. Running in Mock DB mode.")
		mockRepo := &MockJobRepoForServer{}
		seedMockJobs(mockRepo, 1536)
		repo = mockRepo
	}

	// 3. Initialize Redis
	redisURL := *redisURLFlag
	if redisURL == "" {
		redisURL = os.Getenv("REDIS_URL")
	}
	if redisURL == "" {
		redisURL = "localhost:6379"
	}
	log.Printf("Connecting to Redis at %s...", redisURL)
	rdb := redis.NewClient(&redis.Options{
		Addr: redisURL,
	})
	if err := rdb.Ping(ctx).Err(); err != nil {
		log.Printf("Warning: Redis connection failed: %v. Background workflow engine is disabled.", err)
	}

	// 4. Initialize and Start background engine
	var wfStore store.WorkflowStore
	var engine *orchestrator.Engine
	var engineCancel context.CancelFunc

	if pool != nil && rdb != nil {
		wfStore = store.NewPostgresWorkflowStore(pool)
		taskQueue := queue.NewRedisQueue(rdb, "orchid:task_queue")

		registry := domain.NewRegistry()
		_ = registry.Register(tasks.NewIngestTask(repo, client))
		_ = registry.Register(tasks.NewMatchTask(repo, client))
		_ = registry.Register(tasks.NewTailorTask())
		_ = registry.Register(tasks.NewApplyTask())

		engine = orchestrator.NewEngine(wfStore, taskQueue, registry)
		engine.RegisterPlan("job-hunt", orchestrator.WorkflowPlan{
			Steps: []string{"ingest", "match", "tailor", "apply"},
		})

		var engineCtx context.Context
		engineCtx, engineCancel = context.WithCancel(context.Background())
		go engine.Start(engineCtx, 3) // Run with 3 concurrent workers
		log.Println("Background Workflow Engine started with 3 workers.")
	}

	srv := &Server{
		client:  client,
		repo:    repo,
		pool:    pool,
		wfStore: wfStore,
		engine:  engine,
	}

	// Register HTTP Endpoints
	mux := http.NewServeMux()
	mux.HandleFunc("/api/embed", srv.handleEmbed)
	mux.HandleFunc("/api/match", srv.handleMatch)
	mux.HandleFunc("/api/seed", srv.handleSeed)
	mux.HandleFunc("/api/runs", srv.handleRuns)
	mux.HandleFunc("/health", srv.handleHealth)
	mux.HandleFunc("/", srv.handleDashboard)

	addr := fmt.Sprintf("localhost:%d", *port)
	log.Printf("Orchid Server starting on http://%s\n", addr)
	log.Printf("Endpoints:\n  POST http://%s/api/runs\n  GET  http://%s/api/runs\n  GET  http://%s/api/runs?id=<id>\n  POST http://%s/api/seed\n  GET  http://%s/health\n", addr, addr, addr, addr, addr)

	if err := http.ListenAndServe(addr, mux); err != nil {
		log.Fatalf("Server failed: %v", err)
	}

	if engineCancel != nil {
		engineCancel()
	}
}

// handleRuns routes GET and POST requests for workflow run executions.
func (s *Server) handleRuns(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Access-Control-Allow-Origin", "*")
	w.Header().Set("Access-Control-Allow-Methods", "GET, POST, OPTIONS")
	w.Header().Set("Access-Control-Allow-Headers", "Content-Type")

	if r.Method == http.MethodOptions {
		w.WriteHeader(http.StatusOK)
		return
	}

	if s.engine == nil || s.wfStore == nil {
		http.Error(w, "Workflow Engine is not configured (requires Redis and Postgres).", http.StatusInternalServerError)
		return
	}

	switch r.Method {
	case http.MethodPost:
		s.handleSubmitRun(w, r)
	case http.MethodGet:
		s.handleGetRuns(w, r)
	default:
		http.Error(w, "Method not allowed. Use GET or POST.", http.StatusMethodNotAllowed)
	}
}

func (s *Server) handleSubmitRun(w http.ResponseWriter, r *http.Request) {
	body, err := io.ReadAll(r.Body)
	if err != nil {
		http.Error(w, "Failed to read request body", http.StatusBadRequest)
		return
	}
	defer r.Body.Close()

	var req RunSubmissionRequest
	if err := json.Unmarshal(body, &req); err != nil {
		http.Error(w, "Malformed JSON request body", http.StatusBadRequest)
		return
	}

	if req.UserID == "" {
		http.Error(w, "Field 'user_id' is required", http.StatusBadRequest)
		return
	}
	if req.ResumeText == "" {
		http.Error(w, "Field 'resume_text' is required", http.StatusBadRequest)
		return
	}

	initialInput := map[string]any{
		"resume_text": req.ResumeText,
	}

	runID, err := s.engine.SubmitRun(r.Context(), req.UserID, "job-hunt", initialInput)
	if err != nil {
		http.Error(w, fmt.Sprintf("Failed to submit workflow run: %v", err), http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusAccepted)
	_, _ = w.Write([]byte(fmt.Sprintf(`{"status":"submitted","run_id":%d}`, runID)))
}

func (s *Server) handleGetRuns(w http.ResponseWriter, r *http.Request) {
	idStr := r.URL.Query().Get("id")
	if idStr != "" {
		runID, err := strconv.ParseInt(idStr, 10, 64)
		if err != nil {
			http.Error(w, "Invalid run ID format", http.StatusBadRequest)
			return
		}

		run, err := s.wfStore.GetRun(r.Context(), runID)
		if err != nil {
			http.Error(w, fmt.Sprintf("Failed to retrieve run: %v", err), http.StatusInternalServerError)
			return
		}
		if run == nil {
			http.Error(w, "Run not found", http.StatusNotFound)
			return
		}

		steps, err := s.wfStore.GetTaskRunsForRun(r.Context(), runID)
		if err != nil {
			http.Error(w, fmt.Sprintf("Failed to retrieve task runs: %v", err), http.StatusInternalServerError)
			return
		}

		response := map[string]any{
			"run":   run,
			"steps": steps,
		}

		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(response)
		return
	}

	runs, err := s.wfStore.ListRuns(r.Context())
	if err != nil {
		http.Error(w, fmt.Sprintf("Failed to list workflow runs: %v", err), http.StatusInternalServerError)
		return
	}

	if runs == nil {
		runs = []store.WorkflowRun{}
	}

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(runs)
}

func (s *Server) handleEmbed(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed. Use POST.", http.StatusMethodNotAllowed)
		return
	}

	if s.client == nil {
		http.Error(w, "Embedding client is not configured.", http.StatusInternalServerError)
		return
	}

	body, err := io.ReadAll(r.Body)
	if err != nil {
		http.Error(w, "Failed to read request body", http.StatusBadRequest)
		return
	}
	defer r.Body.Close()

	embTask := task.NewEmbeddingTask(s.client)
	output, err := embTask.Execute(r.Context(), body)
	if err != nil {
		http.Error(w, fmt.Sprintf("Embedding failed: %v", err), http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write(output)
}

func (s *Server) handleMatch(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed. Use POST.", http.StatusMethodNotAllowed)
		return
	}

	body, err := io.ReadAll(r.Body)
	if err != nil {
		http.Error(w, "Failed to read request body", http.StatusBadRequest)
		return
	}
	defer r.Body.Close()

	matchTask := task.NewMatchingTask(s.repo, s.client)
	output, err := matchTask.Execute(r.Context(), body)
	if err != nil {
		http.Error(w, fmt.Sprintf("Job matching failed: %v", err), http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write(output)
}

func (s *Server) handleSeed(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed. Use POST.", http.StatusMethodNotAllowed)
		return
	}

	if s.pool == nil {
		http.Error(w, "Database seeding requires a real database connection pool.", http.StatusBadRequest)
		return
	}

	if s.client == nil {
		http.Error(w, "Database seeding requires an active embedding client to store actual job embeddings.", http.StatusBadRequest)
		return
	}

	log.Println("Seeding database via HTTP request...")
	_, err := s.pool.Exec(r.Context(), `
		CREATE EXTENSION IF NOT EXISTS "uuid-ossp";
		CREATE EXTENSION IF NOT EXISTS vector;
		CREATE TABLE IF NOT EXISTS jobs (
			id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
			title VARCHAR(255) NOT NULL,
			company VARCHAR(255) NOT NULL,
			location VARCHAR(255),
			description TEXT NOT NULL,
			embedding VECTOR,
			created_at TIMESTAMP WITH TIME ZONE DEFAULT CURRENT_TIMESTAMP,
			updated_at TIMESTAMP WITH TIME ZONE DEFAULT CURRENT_TIMESTAMP
		);
	`)
	if err != nil {
		http.Error(w, fmt.Sprintf("Failed to initialize database tables: %v", err), http.StatusInternalServerError)
		return
	}

	_, _ = s.pool.Exec(r.Context(), "DELETE FROM jobs WHERE company = 'DemoCorp'")
	
	jobs := []repository.Job{
		{
			Title:       "Backend Go Engineer",
			Company:     "DemoCorp",
			Location:    "Remote (US)",
			Description: "We are seeking a Go developer experienced with PostgreSQL, pgvector, and Redis to build workflow engines.",
		},
		{
			Title:       "React Frontend Specialist",
			Company:     "DemoCorp",
			Location:    "Remote (EU)",
			Description: "Build interactive dashboard interfaces using React, TypeScript, TailwindCSS, and state management tools.",
		},
		{
			Title:       "Senior Staff Software Engineer - Go/DB",
			Company:     "DemoCorp",
			Location:    "San Francisco, CA",
			Description: "High-scale Go system designer. Strong knowledge of database indices, transaction pools, and background worker queues in Golang.",
		},
		{
			Title:       "Technical Product Manager",
			Company:     "DemoCorp",
			Location:    "Austin, TX",
			Description: "Lead job-search automation product direction. Bridge engineering teams, client API requirements, and roadmap planning.",
		},
	}

	for i := range jobs {
		j := &jobs[i]
		v, err := s.client.GetEmbedding(r.Context(), j.Description)
		if err != nil {
			http.Error(w, fmt.Sprintf("Failed to generate embedding for job '%s': %v", j.Title, err), http.StatusInternalServerError)
			return
		}
		j.Embedding = v
		if err := s.repo.InsertJob(r.Context(), j); err != nil {
			http.Error(w, fmt.Sprintf("Failed to insert seed job '%s': %v", j.Title, err), http.StatusInternalServerError)
			return
		}
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write([]byte(`{"status":"success","message":"Seeded 4 jobs successfully"}`))
}

func (s *Server) handleHealth(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write([]byte(`{"status":"healthy"}`))
}

// MockJobRepoForServer provides mock matching logic for server tests.
type MockJobRepoForServer struct {
	jobs []repository.Job
}

func (m *MockJobRepoForServer) InsertJob(ctx context.Context, job *repository.Job) error {
	m.jobs = append(m.jobs, *job)
	return nil
}

func (m *MockJobRepoForServer) MatchJobs(ctx context.Context, vector []float32, limit int, minSimilarity float64) ([]repository.JobMatch, error) {
	var matches []repository.JobMatch
	for _, j := range m.jobs {
		similarity := dotProduct(vector, j.Embedding)
		normSimilarity := float64(0.6 + 0.35*(similarity+1.0)/2.0)
		if normSimilarity > minSimilarity {
			matches = append(matches, repository.JobMatch{
				Job:        j,
				Similarity: normSimilarity,
			})
		}
	}
	for i := 0; i < len(matches); i++ {
		for j := i + 1; j < len(matches); j++ {
			if matches[i].Similarity < matches[j].Similarity {
				matches[i], matches[j] = matches[j], matches[i]
			}
		}
	}
	if len(matches) > limit {
		matches = matches[:limit]
	}
	return matches, nil
}

func seedMockJobs(repo *MockJobRepoForServer, dim int) {
	descriptions := []string{
		"We are seeking a Go developer experienced with PostgreSQL, pgvector, and Redis to build workflow engines.",
		"Build interactive dashboard interfaces using React, TypeScript, TailwindCSS, and state management tools.",
		"High-scale Go system designer. Strong knowledge of database indices, transaction pools, and background worker queues in Golang.",
		"Lead job-search automation product direction. Bridge engineering teams, client API requirements, and roadmap planning.",
	}
	titles := []string{"Backend Go Engineer", "React Frontend Specialist", "Senior Staff Software Engineer - Go/DB", "Technical Product Manager"}
	locations := []string{"Remote (US)", "Remote (EU)", "San Francisco, CA", "Austin, TX"}

	for i := 0; i < 4; i++ {
		v := make([]float32, dim)
		for d := 0; d < dim; d++ {
			v[d] = float32((i*7 + d*13) % 100) / 100.0
		}
		repo.jobs = append(repo.jobs, repository.Job{
			ID:          fmt.Sprintf("%d0000000-0000-0000-0000-000000000000", i+1),
			Title:       titles[i],
			Company:     "DemoCorp",
			Location:    "MockCity",
			Description: descriptions[i] + " (" + locations[i] + ")",
			Embedding:   v,
		})
	}
}

func dotProduct(a, b []float32) float32 {
	if len(a) != len(b) {
		return 0
	}
	var sum float32
	for i := range a {
		sum += a[i] * b[i]
	}
	return sum
}

// handleDashboard serves the Orchid visual progress tracker dashboard.
func (s *Server) handleDashboard(w http.ResponseWriter, r *http.Request) {
	if r.URL.Path != "/" {
		http.NotFound(w, r)
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	_, _ = w.Write([]byte(dashboardHTML))
}


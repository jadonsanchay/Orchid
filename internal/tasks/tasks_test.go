package tasks

import (
	"context"
	"fmt"
	"sync"
	"testing"

	"orchid/internal/domain"
	"orchid/pkg/embedding"
	"orchid/pkg/repository"
)

// mockEmbeddingClient implements embedding.Client for unit testing tasks offline.
type mockEmbeddingClient struct {
	dim int
}

func (m *mockEmbeddingClient) GetEmbedding(ctx context.Context, text string) ([]float32, error) {
	v := make([]float32, m.dim)
	for i := 0; i < m.dim; i++ {
		v[i] = float32(i+1) / float32(m.dim)
	}
	return v, nil
}

func (m *mockEmbeddingClient) Provider() embedding.Provider { return embedding.Provider("mock") }
func (m *mockEmbeddingClient) Dimension() int               { return m.dim }

// mockJobRepository implements repository.JobRepository in-memory for testing.
type mockJobRepository struct {
	mu   sync.Mutex
	jobs []repository.Job
}

func (m *mockJobRepository) InsertJob(ctx context.Context, job *repository.Job) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	job.ID = fmt.Sprintf("job-%d", len(m.jobs)+1)
	m.jobs = append(m.jobs, *job)
	return nil
}

func (m *mockJobRepository) MatchJobs(ctx context.Context, vector []float32, limit int, minSimilarity float64) ([]repository.JobMatch, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	var matches []repository.JobMatch
	for _, j := range m.jobs {
		matches = append(matches, repository.JobMatch{
			Job:        j,
			Similarity: 0.88,
		})
	}
	if len(matches) > limit {
		matches = matches[:limit]
	}
	return matches, nil
}

func TestConcreteTasks_WorkflowChaining(t *testing.T) {
	ctx := context.Background()
	mockRepo := &mockJobRepository{}
	mockEmb := &mockEmbeddingClient{dim: 768}

	// 1. Ingest Task
	ingest := NewIngestTask(mockRepo, mockEmb)
	inputData := map[string]any{
		"resume_text": "Experienced Go engineer who loves microservices and Redis.",
	}
	in := domain.TaskInput{
		RunID:  100,
		UserID: "user-1",
		Data:   inputData,
	}

	outIngest, err := ingest.Execute(ctx, in)
	if err != nil {
		t.Fatalf("IngestTask failed: %v", err)
	}
	if outIngest.Data["ingested_count"] != 4.0 {
		t.Errorf("expected 4 ingested jobs, got %v", outIngest.Data["ingested_count"])
	}
	// Verify passthrough
	if outIngest.Data["resume_text"] != inputData["resume_text"] {
		t.Error("IngestTask failed to pass through resume_text")
	}

	// 2. Match Task
	match := NewMatchTask(mockRepo, mockEmb)
	inMatch := domain.TaskInput{
		RunID:  100,
		UserID: "user-1",
		Data:   outIngest.Data,
	}
	outMatch, err := match.Execute(ctx, inMatch)
	if err != nil {
		t.Fatalf("MatchTask failed: %v", err)
	}
	matchedJobs, ok := outMatch.Data["matched_jobs"].([]map[string]any)
	if !ok || len(matchedJobs) != 3 {
		t.Fatalf("expected 3 matched jobs in output, got %+v", outMatch.Data["matched_jobs"])
	}

	// 3. Tailor Task
	tailor := NewTailorTask()
	inTailor := domain.TaskInput{
		RunID:  100,
		UserID: "user-1",
		Data:   outMatch.Data,
	}
	outTailor, err := tailor.Execute(ctx, inTailor)
	if err != nil {
		t.Fatalf("TailorTask failed: %v", err)
	}
	tailoredResume, ok := outTailor.Data["tailored_resume"].(string)
	if !ok || tailoredResume == "" {
		t.Fatal("TailorTask failed to produce tailored_resume")
	}
	targetJobID, ok := outTailor.Data["target_job_id"].(string)
	if !ok || targetJobID != "job-1" {
		t.Errorf("expected target_job_id to be 'job-1', got %v", outTailor.Data["target_job_id"])
	}

	// 4. Apply Task
	apply := NewApplyTask()
	inApply := domain.TaskInput{
		RunID:  100,
		UserID: "user-1",
		Data:   outTailor.Data,
	}
	outApply, err := apply.Execute(ctx, inApply)
	if err != nil {
		t.Fatalf("ApplyTask failed: %v", err)
	}
	if outApply.Data["status"] != "submitted" {
		t.Errorf("expected status 'submitted', got %v", outApply.Data["status"])
	}
	if outApply.Data["applied_job_id"] != "job-1" {
		t.Errorf("expected applied_job_id 'job-1', got %v", outApply.Data["applied_job_id"])
	}
	if outApply.Data["confirmation_code"] != "APP-100-MOCK" {
		t.Errorf("expected confirmation 'APP-100-MOCK', got %v", outApply.Data["confirmation_code"])
	}
}

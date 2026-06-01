package task

import (
	"context"
	"encoding/json"
	"errors"
	"testing"

	"orchid/pkg/embedding"
	"orchid/pkg/repository"
	"orchid/pkg/testutils"
)

// MockJobRepository is an in-memory mock repository for testing task steps.
type MockJobRepository struct {
	Jobs           []repository.Job
	InsertErr      error
	MatchErr       error
	LastInsertJob  *repository.Job
	LastMatchVec   []float32
	LastMatchLimit int
	LastMinScore   float64
}

func (m *MockJobRepository) InsertJob(ctx context.Context, job *repository.Job) error {
	if m.InsertErr != nil {
		return m.InsertErr
	}
	m.LastInsertJob = job
	m.Jobs = append(m.Jobs, *job)
	return nil
}

func (m *MockJobRepository) MatchJobs(ctx context.Context, vector []float32, limit int, minSimilarity float64) ([]repository.JobMatch, error) {
	if m.MatchErr != nil {
		return nil, m.MatchErr
	}
	m.LastMatchVec = vector
	m.LastMatchLimit = limit
	m.LastMinScore = minSimilarity

	var matches []repository.JobMatch
	for _, j := range m.Jobs {
		matches = append(matches, repository.JobMatch{
			Job:        j,
			Similarity: 0.85, // Stub a matching score
		})
	}
	return matches, nil
}

func TestEmbeddingTask(t *testing.T) {
	mockClient := testutils.NewMockEmbeddingClient(embedding.ProviderOpenAI, 1536)
	mockClient.DefaultResult = make([]float32, 1536)
	mockClient.DefaultResult[0] = 0.5
	mockClient.DefaultResult[1] = -0.5

	embeddingTask := NewEmbeddingTask(mockClient)

	t.Run("successful execution", func(t *testing.T) {
		input := []byte(`{"text": "Sample resume details"}`)
		outputBytes, err := embeddingTask.Execute(context.Background(), input)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		var output EmbeddingTaskOutput
		if err := json.Unmarshal(outputBytes, &output); err != nil {
			t.Fatalf("failed to unmarshal output: %v", err)
		}

		if output.Provider != "openai" {
			t.Errorf("expected provider 'openai', got '%s'", output.Provider)
		}
		if len(output.Embedding) != 1536 {
			t.Errorf("expected embedding dimension 1536, got %d", len(output.Embedding))
		}
		if output.Embedding[0] != 0.5 || output.Embedding[1] != -0.5 {
			t.Errorf("embedding values did not match stubbed values")
		}
	})

	t.Run("empty text error", func(t *testing.T) {
		input := []byte(`{"text": ""}`)
		_, err := embeddingTask.Execute(context.Background(), input)
		if err == nil {
			t.Error("expected error for empty text, got nil")
		}
	})

	t.Run("malformed input error", func(t *testing.T) {
		input := []byte(`invalid json`)
		_, err := embeddingTask.Execute(context.Background(), input)
		if err == nil {
			t.Error("expected error for malformed JSON, got nil")
		}
	})
}

func TestMatchingTask(t *testing.T) {
	mockRepo := &MockJobRepository{
		Jobs: []repository.Job{
			{
				ID:          "11111111-2222-3333-4444-555555555555",
				Title:       "Software Engineer",
				Company:     "Orchid Tech",
				Description: "Go backend developer",
			},
		},
	}

	mockClient := testutils.NewMockEmbeddingClient(embedding.ProviderGemini, 768)
	mockClient.DefaultResult = make([]float32, 768)

	matchingTask := NewMatchingTask(mockRepo, mockClient)

	t.Run("match using precalculated embedding", func(t *testing.T) {
		inputVec := make([]float32, 768)
		inputVec[0] = 0.9
		inputPayload := MatchingTaskInput{
			Embedding: inputVec,
			Limit:     2,
			MinScore:  0.80,
		}

		inputBytes, _ := json.Marshal(inputPayload)
		outputBytes, err := matchingTask.Execute(context.Background(), inputBytes)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		var output MatchingTaskOutput
		if err := json.Unmarshal(outputBytes, &output); err != nil {
			t.Fatalf("failed to unmarshal output: %v", err)
		}

		if len(output.Matches) != 1 {
			t.Fatalf("expected 1 match, got %d", len(output.Matches))
		}

		match := output.Matches[0]
		if match.Title != "Software Engineer" {
			t.Errorf("expected job title 'Software Engineer', got '%s'", match.Title)
		}
		if match.Similarity != 0.85 {
			t.Errorf("expected similarity score 0.85, got %f", match.Similarity)
		}
		if mockRepo.LastMatchLimit != 2 {
			t.Errorf("expected limit 2 passed to repo, got %d", mockRepo.LastMatchLimit)
		}
		if mockRepo.LastMinScore != 0.80 {
			t.Errorf("expected min score 0.80, got %f", mockRepo.LastMinScore)
		}
	})

	t.Run("match generating embedding dynamically from text", func(t *testing.T) {
		inputPayload := MatchingTaskInput{
			Text: "Resume text",
		}

		inputBytes, _ := json.Marshal(inputPayload)
		_, err := matchingTask.Execute(context.Background(), inputBytes)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		// Defaults should apply: Limit = 5, MinScore = 0.75
		if mockRepo.LastMatchLimit != 5 {
			t.Errorf("expected default limit 5, got %d", mockRepo.LastMatchLimit)
		}
		if mockRepo.LastMinScore != 0.75 {
			t.Errorf("expected default min similarity 0.75, got %f", mockRepo.LastMinScore)
		}
	})

	t.Run("text matching fails when client is missing", func(t *testing.T) {
		matchingTaskNoClient := NewMatchingTask(mockRepo, nil)
		inputPayload := MatchingTaskInput{
			Text: "Resume text",
		}
		inputBytes, _ := json.Marshal(inputPayload)
		_, err := matchingTaskNoClient.Execute(context.Background(), inputBytes)
		if err == nil {
			t.Error("expected error due to missing client, got nil")
		}
	})

	t.Run("missing both text and embedding", func(t *testing.T) {
		inputBytes := []byte(`{"limit": 5}`)
		_, err := matchingTask.Execute(context.Background(), inputBytes)
		if err == nil {
			t.Error("expected error for missing input data, got nil")
		}
	})

	t.Run("repository error is bubbled", func(t *testing.T) {
		mockRepoErr := &MockJobRepository{
			MatchErr: errors.New("db error"),
		}
		taskWithErr := NewMatchingTask(mockRepoErr, nil)
		inputVec := make([]float32, 768)
		inputBytes, _ := json.Marshal(MatchingTaskInput{Embedding: inputVec})
		_, err := taskWithErr.Execute(context.Background(), inputBytes)
		if err == nil {
			t.Error("expected error from repository call, got nil")
		}
	})
}

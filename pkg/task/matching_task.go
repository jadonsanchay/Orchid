package task

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"

	"orchid/pkg/embedding"
	"orchid/pkg/repository"
)

// MatchingTask is a pluggable workflow step that finds jobs matching a resume.
// It can accept a pre-calculated vector or raw text (generating the embedding dynamically).
type MatchingTask struct {
	repo            repository.JobRepository
	embeddingClient embedding.Client // Optional: only needed if text input is allowed
}

// NewMatchingTask creates a MatchingTask with the repository and optional embedding client.
func NewMatchingTask(repo repository.JobRepository, client embedding.Client) *MatchingTask {
	return &MatchingTask{
		repo:            repo,
		embeddingClient: client,
	}
}

// Name returns the task's registration name.
func (t *MatchingTask) Name() string {
	return "match-jobs"
}

// MatchingTaskInput defines the JSON input structure for the matching task.
type MatchingTaskInput struct {
	Embedding []float32 `json:"embedding,omitempty"`      // Precalculated vector embedding
	Text      string    `json:"text,omitempty"`           // Raw text (if vector is not pre-calculated)
	Limit     int       `json:"limit,omitempty"`          // Max matches to return (defaults to 5)
	MinScore  float64   `json:"min_similarity,omitempty"` // Minimum similarity score threshold (defaults to 0.75)
}

// MatchingTaskOutput defines the JSON output structure containing matched jobs list.
type MatchingTaskOutput struct {
	Matches []repository.JobMatch `json:"matches"`
}

// Execute unmarshals the JSON input, processes vector/text, queries the DB repository, and returns JSON output.
func (t *MatchingTask) Execute(ctx context.Context, input []byte) ([]byte, error) {
	if len(input) == 0 {
		return nil, errors.New("empty input payload")
	}

	var in MatchingTaskInput
	if err := json.Unmarshal(input, &in); err != nil {
		return nil, fmt.Errorf("failed to unmarshal input payload: %w", err)
	}

	// 1. Resolve embedding vector
	var vector []float32
	if len(in.Embedding) > 0 {
		vector = in.Embedding
	} else if in.Text != "" {
		if t.embeddingClient == nil {
			return nil, errors.New("cannot process text input: embedding client is not configured for matching task")
		}
		var err error
		vector, err = t.embeddingClient.GetEmbedding(ctx, in.Text)
		if err != nil {
			return nil, fmt.Errorf("failed to generate embedding dynamically: %w", err)
		}
	} else {
		return nil, errors.New("input must provide either 'embedding' vector or 'text' query")
	}

	// 2. Set defaults
	limit := in.Limit
	if limit <= 0 {
		limit = 5
	}
	minSimilarity := in.MinScore
	if minSimilarity <= 0 {
		minSimilarity = 0.75 // Default similarity score matching requirement
	}

	// 3. Query repository
	matches, err := t.repo.MatchJobs(ctx, vector, limit, minSimilarity)
	if err != nil {
		return nil, fmt.Errorf("failed to match jobs: %w", err)
	}

	// Ensure we return an empty array instead of null in JSON if there are no matches
	if matches == nil {
		matches = []repository.JobMatch{}
	}

	out := MatchingTaskOutput{
		Matches: matches,
	}

	outBytes, err := json.Marshal(out)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal task output: %w", err)
	}

	return outBytes, nil
}

package tasks

import (
	"context"
	"errors"
	"fmt"
	"log"

	"orchid/internal/domain"
	"orchid/pkg/embedding"
	"orchid/pkg/repository"
)

// MatchTask is a pluggable workflow step that calculates the embedding of the user's
// resume and queries the job repository for matches using vector similarity search.
type MatchTask struct {
	repo            repository.JobRepository
	embeddingClient embedding.Client
}

// NewMatchTask constructs a new MatchTask.
func NewMatchTask(repo repository.JobRepository, client embedding.Client) *MatchTask {
	return &MatchTask{
		repo:            repo,
		embeddingClient: client,
	}
}

// Name returns the registration identifier for the match task.
func (t *MatchTask) Name() string {
	return "match"
}

// Execute performs cosine similarity queries to match the input resume text against jobs.
func (t *MatchTask) Execute(ctx context.Context, in domain.TaskInput) (domain.TaskOutput, error) {
	log.Printf("[MatchTask] Starting job matching for Run %d", in.RunID)

	resumeText, ok := in.Data["resume_text"].(string)
	if !ok || resumeText == "" {
		return domain.TaskOutput{}, errors.New("missing or empty 'resume_text' in input data")
	}

	limit := 3
	if l, exists := in.Data["limit"].(float64); exists {
		limit = int(l)
	}

	minSimilarity := 0.70
	if s, exists := in.Data["min_similarity"].(float64); exists {
		minSimilarity = s
	}

	// 1. Generate embedding vector for resume
	resumeVector, err := t.embeddingClient.GetEmbedding(ctx, resumeText)
	if err != nil {
		return domain.TaskOutput{}, fmt.Errorf("failed to generate embedding for resume: %w", err)
	}

	// 2. Query repository for matches
	matches, err := t.repo.MatchJobs(ctx, resumeVector, limit, minSimilarity)
	if err != nil {
		return domain.TaskOutput{}, fmt.Errorf("failed to query matching jobs: %w", err)
	}

	log.Printf("[MatchTask] Found %d matching jobs for Run %d", len(matches), in.RunID)

	// Prepare output with passthrough data pattern
	outData := make(map[string]any)
	for k, v := range in.Data {
		outData[k] = v
	}

	// Convert JobMatches to a simple JSON-compatible map slice for state persistence
	var matchMapList []map[string]any
	for _, m := range matches {
		matchMap := map[string]any{
			"id":          m.ID,
			"title":       m.Title,
			"company":     m.Company,
			"location":    m.Location,
			"description": m.Description,
			"similarity":  m.Similarity,
		}
		matchMapList = append(matchMapList, matchMap)
	}
	outData["matched_jobs"] = matchMapList

	return domain.TaskOutput{Data: outData}, nil
}

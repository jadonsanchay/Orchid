// Package tasks contains the concrete task steps executed by the Orchid orchestrator engine.
package tasks

import (
	"context"
	"fmt"
	"log"

	"orchid/internal/domain"
	"orchid/pkg/embedding"
	"orchid/pkg/repository"
)

// IngestTask is a pluggable workflow step that seeds jobs, generates their embeddings,
// and saves them to the repository database.
type IngestTask struct {
	repo            repository.JobRepository
	embeddingClient embedding.Client
}

// NewIngestTask constructs a new IngestTask.
func NewIngestTask(repo repository.JobRepository, client embedding.Client) *IngestTask {
	return &IngestTask{
		repo:            repo,
		embeddingClient: client,
	}
}

// Name returns the registration identifier for the ingest task.
func (t *IngestTask) Name() string {
	return "ingest"
}

// Execute runs the ingestion logic by fetching jobs, calculating vector embeddings,
// and persisting them in PostgreSQL. It propagates unconsumed inputs to its output.
func (t *IngestTask) Execute(ctx context.Context, in domain.TaskInput) (domain.TaskOutput, error) {
	log.Printf("[IngestTask] Starting job ingestion for Run %d", in.RunID)

	// Define sample jobs to seed
	jobsToIngest := []repository.Job{
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

	// Calculate embeddings and store jobs
	for i := range jobsToIngest {
		job := &jobsToIngest[i]
		emb, err := t.embeddingClient.GetEmbedding(ctx, job.Description)
		if err != nil {
			return domain.TaskOutput{}, fmt.Errorf("failed to generate embedding for job '%s': %w", job.Title, err)
		}
		job.Embedding = emb

		err = t.repo.InsertJob(ctx, job)
		if err != nil {
			return domain.TaskOutput{}, fmt.Errorf("failed to insert job '%s': %w", job.Title, err)
		}
	}

	log.Printf("[IngestTask] Successfully ingested %d jobs for Run %d", len(jobsToIngest), in.RunID)

	// Prepare output with passthrough data pattern
	outData := make(map[string]any)
	for k, v := range in.Data {
		outData[k] = v
	}
	outData["ingested_count"] = float64(len(jobsToIngest))

	return domain.TaskOutput{Data: outData}, nil
}

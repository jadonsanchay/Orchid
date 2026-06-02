package repository

import (
	"context"
	"os"
	"testing"

	"orchid/pkg/utils"

	"github.com/jackc/pgx/v5"
)

func TestPostgresJobRepository_Integration(t *testing.T) {
	utils.LoadEnv() // Load .env file configurations locally
	// The integration test only runs if a test database URL is supplied.
	// Example: TEST_DATABASE_URL="postgres://postgres:password@localhost:5432/postgres?sslmode=disable"
	dbURL := os.Getenv("TEST_DATABASE_URL")
	if dbURL == "" {
		t.Skip("Skipping PostgreSQL integration test. Set TEST_DATABASE_URL to run.")
	}

	ctx := context.Background()
	conn, err := pgx.Connect(ctx, dbURL)
	if err != nil {
		t.Fatalf("failed to connect to database: %v", err)
	}
	defer conn.Close(ctx)

	// Set up schema dynamically for testing (mirroring schema.sql)
	_, err = conn.Exec(ctx, `
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
		t.Fatalf("failed to create test jobs table: %v", err)
	}

	// Clean table before and after run
	cleanup := func() {
		_, _ = conn.Exec(ctx, "DELETE FROM jobs WHERE company = $1", "IntegrationTestCorp")
	}
	cleanup()
	defer cleanup()

	repo := NewPostgresJobRepository(conn)

	// Since we use a dimension-less VECTOR column, we can test with lightweight 3-dimensional vectors.
	// Cosine Similarity = A . B / (||A|| ||B||)
	//
	// Let's create vectors:
	// Query Vector: [1.0, 0.0, 0.0]
	//
	// Job A: Embedding = [1.0, 0.1, 0.0] (Very similar to Query)
	// Cosine Similarity with query is ~0.995 (expected score > 0.75)
	//
	// Job B: Embedding = [0.0, 1.0, 0.0] (Orthogonal, completely different)
	// Cosine Similarity with query is 0.0 (expected score < 0.75)
	//
	// Job C: Embedding = [0.7, 0.7, 0.0] (Partially similar)
	// Cosine Similarity with query is 0.707 (expected score < 0.75)

	jobA := &Job{
		Title:       "High Match Developer",
		Company:     "IntegrationTestCorp",
		Location:    "Remote",
		Description: "Looking for Golang experts",
		Embedding:   []float32{1.0, 0.1, 0.0},
	}
	jobB := &Job{
		Title:       "Low Match Marketer",
		Company:     "IntegrationTestCorp",
		Location:    "New York",
		Description: "Marketing copywriter",
		Embedding:   []float32{0.0, 1.0, 0.0},
	}
	jobC := &Job{
		Title:       "Medium Match PM",
		Company:     "IntegrationTestCorp",
		Location:    "San Francisco",
		Description: "Product management",
		Embedding:   []float32{0.7, 0.7, 0.0},
	}

	if err := repo.InsertJob(ctx, jobA); err != nil {
		t.Fatalf("failed to insert job A: %v", err)
	}
	if err := repo.InsertJob(ctx, jobB); err != nil {
		t.Fatalf("failed to insert job B: %v", err)
	}
	if err := repo.InsertJob(ctx, jobC); err != nil {
		t.Fatalf("failed to insert job C: %v", err)
	}

	// Perform matching
	queryVector := []float32{1.0, 0.0, 0.0}
	matches, err := repo.MatchJobs(ctx, queryVector, 10, 0.75)
	if err != nil {
		t.Fatalf("failed to run MatchJobs: %v", err)
	}

	// Verify only Job A matches (similarity > 0.75)
	if len(matches) != 1 {
		t.Fatalf("expected exactly 1 match (Job A), got %d matches", len(matches))
	}

	bestMatch := matches[0]
	if bestMatch.Title != "High Match Developer" {
		t.Errorf("expected best match to be 'High Match Developer', got '%s'", bestMatch.Title)
	}
	if bestMatch.Similarity < 0.99 {
		t.Errorf("expected similarity score near 0.995, got %f", bestMatch.Similarity)
	}

	// Let's verify we can find the medium match if we lower the threshold to 0.70
	matchesLow, err := repo.MatchJobs(ctx, queryVector, 10, 0.70)
	if err != nil {
		t.Fatalf("failed to run MatchJobs with lower score: %v", err)
	}

	// Expecting Job A and Job C (which has similarity 0.707)
	if len(matchesLow) != 2 {
		t.Errorf("expected exactly 2 matches (Job A and Job C) at threshold 0.70, got %d", len(matchesLow))
	}
}

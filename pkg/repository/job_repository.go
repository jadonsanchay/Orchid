// Package repository handles database operations for job data stored in Postgres.
package repository

import (
	"context"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/pgvector/pgvector-go"
)

// DBExecutor interface represents the common subset of methods shared between
// a direct connection, a connection pool, and a transaction.
type DBExecutor interface {
	Exec(ctx context.Context, sql string, arguments ...any) (pgconn.CommandTag, error)
	Query(ctx context.Context, sql string, args ...any) (pgx.Rows, error)
	QueryRow(ctx context.Context, sql string, args ...any) pgx.Row
}

// JobRepository defines the repository interface for job persistence and vector operations.
type JobRepository interface {
	// InsertJob inserts a new Job record with its embedding vector into the database.
	InsertJob(ctx context.Context, job *Job) error

	// MatchJobs executes a cosine distance search to find jobs resembling the given vector.
	// Returns jobs with similarity scores higher than the minSimilarity threshold.
	MatchJobs(ctx context.Context, vector []float32, limit int, minSimilarity float64) ([]JobMatch, error)
}

// PostgresJobRepository is a PostgreSQL-backed implementation of JobRepository.
type PostgresJobRepository struct {
	db DBExecutor
}

// NewPostgresJobRepository constructs a new PostgresJobRepository.
func NewPostgresJobRepository(db DBExecutor) *PostgresJobRepository {
	return &PostgresJobRepository{
		db: db,
	}
}

// InsertJob persists a job record in PostgreSQL database.
func (r *PostgresJobRepository) InsertJob(ctx context.Context, job *Job) error {
	if job == nil {
		return errors.New("cannot insert nil job")
	}

	query := `
		INSERT INTO jobs (title, company, location, description, embedding)
		VALUES ($1, $2, $3, $4, $5)
		RETURNING id, created_at, updated_at
	`

	var vecVal *pgvector.Vector
	if len(job.Embedding) > 0 {
		v := pgvector.NewVector(job.Embedding)
		vecVal = &v
	}

	err := r.db.QueryRow(
		ctx,
		query,
		job.Title,
		job.Company,
		job.Location,
		job.Description,
		vecVal,
	).Scan(&job.ID, &job.CreatedAt, &job.UpdatedAt)

	if err != nil {
		return fmt.Errorf("failed to insert job record: %w", err)
	}

	return nil
}

// MatchJobs performs a Cosine Distance query (<=>) to find matching jobs.
// Cosine Similarity = 1 - Cosine Distance.
// We order by distance ascending (which is closest first) and filter by similarity score threshold.
func (r *PostgresJobRepository) MatchJobs(ctx context.Context, vector []float32, limit int, minSimilarity float64) ([]JobMatch, error) {
	if len(vector) == 0 {
		return nil, errors.New("cannot match empty query vector")
	}
	if limit <= 0 {
		limit = 10 // Default limit if unspecified
	}

	query := `
		SELECT id, title, company, location, description, 1 - (embedding <=> $1) as similarity, created_at, updated_at
		FROM jobs
		WHERE 1 - (embedding <=> $1) > $2
		ORDER BY embedding <=> $1
		LIMIT $3
	`

	rows, err := r.db.Query(ctx, query, pgvector.NewVector(vector), minSimilarity, limit)
	if err != nil {
		return nil, fmt.Errorf("failed to query matching jobs: %w", err)
	}
	defer rows.Close()

	var matches []JobMatch
	for rows.Next() {
		var m JobMatch
		err := rows.Scan(
			&m.ID,
			&m.Title,
			&m.Company,
			&m.Location,
			&m.Description,
			&m.Similarity,
			&m.CreatedAt,
			&m.UpdatedAt,
		)
		if err != nil {
			return nil, fmt.Errorf("failed to scan matched job row: %w", err)
		}
		matches = append(matches, m)
	}

	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("error reading match rows: %w", err)
	}

	return matches, nil
}

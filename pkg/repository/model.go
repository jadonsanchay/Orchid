package repository

import "time"

// Job represents a job record stored in the PostgreSQL database.
type Job struct {
	ID          string    `json:"id"`          // UUID of the job
	Title       string    `json:"title"`       // Job title
	Company     string    `json:"company"`     // Company offering the job
	Location    string    `json:"location"`    // Job location (remote/city)
	Description string    `json:"description"` // Job description details
	Embedding   []float32 `json:"-"`           // Embedding vector (excluded from standard JSON output)
	CreatedAt   time.Time `json:"created_at"`
	UpdatedAt   time.Time `json:"updated_at"`
}

// JobMatch represents a matched job returned from similarity queries.
// It embeds the standard Job structure and appends a Similarity score.
type JobMatch struct {
	Job
	Similarity float64 `json:"similarity"` // Cosine similarity score (1 - Cosine Distance)
}

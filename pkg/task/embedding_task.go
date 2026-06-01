package task

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"

	"orchid/pkg/embedding"
)

// EmbeddingTask is a pluggable workflow step that takes a text block 
// (e.g. resume or job description) and calls the configured AI Embedding Client
// to generate its vector representation.
type EmbeddingTask struct {
	client embedding.Client
}

// NewEmbeddingTask creates an EmbeddingTask with the given client.
func NewEmbeddingTask(client embedding.Client) *EmbeddingTask {
	return &EmbeddingTask{
		client: client,
	}
}

// Name returns the task's registration name.
func (t *EmbeddingTask) Name() string {
	return "generate-embedding"
}

// EmbeddingTaskInput defines the JSON input structure for the task.
type EmbeddingTaskInput struct {
	Text string `json:"text"`
}

// EmbeddingTaskOutput defines the JSON output structure containing the resulting vector.
type EmbeddingTaskOutput struct {
	Embedding []float32 `json:"embedding"`
	Provider  string    `json:"provider"`
}

// Execute unmarshals the JSON input, fetches the embedding vector, and returns the JSON output.
func (t *EmbeddingTask) Execute(ctx context.Context, input []byte) ([]byte, error) {
	if len(input) == 0 {
		return nil, errors.New("empty input payload")
	}

	var in EmbeddingTaskInput
	if err := json.Unmarshal(input, &in); err != nil {
		return nil, fmt.Errorf("failed to unmarshal input payload: %w", err)
	}

	if in.Text == "" {
		return nil, errors.New("input text field 'text' cannot be empty")
	}

	vector, err := t.client.GetEmbedding(ctx, in.Text)
	if err != nil {
		return nil, fmt.Errorf("failed to generate embedding: %w", err)
	}

	out := EmbeddingTaskOutput{
		Embedding: vector,
		Provider:  string(t.client.Provider()),
	}

	outBytes, err := json.Marshal(out)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal task output: %w", err)
	}

	return outBytes, nil
}

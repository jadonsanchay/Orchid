package embedding

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"time"
)

// OpenAIClient implements Client for OpenAI embedding services.
type OpenAIClient struct {
	apiKey string
	model  string
	client *http.Client
}

// NewOpenAIClient initializes an OpenAI client wrapper.
func NewOpenAIClient(apiKey, model string) *OpenAIClient {
	return &OpenAIClient{
		apiKey: apiKey,
		model:  model,
		client: &http.Client{
			Timeout: 15 * time.Second,
		},
	}
}

// Provider returns ProviderOpenAI.
func (c *OpenAIClient) Provider() Provider {
	return ProviderOpenAI
}

// Dimension returns the dimension size of the embedding model.
func (c *OpenAIClient) Dimension() int {
	// Standard OpenAI embedding dimensions
	switch c.model {
	case "text-embedding-3-small":
		return 1536
	case "text-embedding-3-large":
		return 3072
	case "text-embedding-ada-002":
		return 1536
	default:
		return 1536 // Default fallback
	}
}

// OpenAIRequest represents the payload structure for the OpenAI embeddings API.
type OpenAIRequest struct {
	Input string `json:"input"`
	Model string `json:"model"`
}

// OpenAIResponse represents the response structure from the OpenAI embeddings API.
type OpenAIResponse struct {
	Data []struct {
		Embedding []float32 `json:"embedding"`
		Index     int       `json:"index"`
	} `json:"data"`
	Error *struct {
		Message string `json:"message"`
		Type    string `json:"type"`
	} `json:"error,omitempty"`
}

// GetEmbedding fetches text embedding vector from OpenAI API.
func (c *OpenAIClient) GetEmbedding(ctx context.Context, text string) ([]float32, error) {
	url := "https://api.openai.com/v1/embeddings"

	reqPayload := OpenAIRequest{
		Input: text,
		Model: c.model,
	}

	jsonData, err := json.Marshal(reqPayload)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal request payload: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, "POST", url, bytes.NewBuffer(jsonData))
	if err != nil {
		return nil, fmt.Errorf("failed to create http request: %w", err)
	}

	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+c.apiKey)

	resp, err := c.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("openai http request failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		var errResp OpenAIResponse
		if err := json.NewDecoder(resp.Body).Decode(&errResp); err == nil && errResp.Error != nil {
			return nil, fmt.Errorf("openai api error (HTTP %d): %s", resp.StatusCode, errResp.Error.Message)
		}
		return nil, fmt.Errorf("openai api returned status: %s", resp.Status)
	}

	var apiResp OpenAIResponse
	if err := json.NewDecoder(resp.Body).Decode(&apiResp); err != nil {
		return nil, fmt.Errorf("failed to decode openai response: %w", err)
	}

	if len(apiResp.Data) == 0 {
		return nil, fmt.Errorf("openai returned empty embedding data list")
	}

	return apiResp.Data[0].Embedding, nil
}

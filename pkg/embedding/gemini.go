package embedding

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"time"
)

// GeminiClient implements Client for Google Gemini embedding services.
type GeminiClient struct {
	apiKey string
	model  string
	client *http.Client
}

// NewGeminiClient initializes a Gemini client wrapper.
func NewGeminiClient(apiKey, model string) *GeminiClient {
	return &GeminiClient{
		apiKey: apiKey,
		model:  model,
		client: &http.Client{
			Timeout: 15 * time.Second,
		},
	}
}

// Provider returns ProviderGemini.
func (c *GeminiClient) Provider() Provider {
	return ProviderGemini
}

// Dimension returns the dimension size of the embedding model.
func (c *GeminiClient) Dimension() int {
	// Standard Gemini embedding dimensions
	switch c.model {
	case "text-embedding-004":
		return 768
	default:
		return 768 // Default fallback for standard Gemini text-embedding models
	}
}

// GeminiPart represents a part of the content.
type GeminiPart struct {
	Text string `json:"text"`
}

// GeminiContent represents the content structure for the Gemini API.
type GeminiContent struct {
	Parts []GeminiPart `json:"parts"`
}

// GeminiRequest represents the payload structure for the Gemini embedContent API.
type GeminiRequest struct {
	Content GeminiContent `json:"content"`
}

// GeminiResponse represents the response structure from the Gemini embedContent API.
type GeminiResponse struct {
	Embedding struct {
		Values []float32 `json:"values"`
	} `json:"embedding"`
	Error *struct {
		Code    int    `json:"code"`
		Message string `json:"message"`
		Status  string `json:"status"`
	} `json:"error,omitempty"`
}

// GetEmbedding fetches text embedding vector from Google Gemini API.
func (c *GeminiClient) GetEmbedding(ctx context.Context, text string) ([]float32, error) {
	// Gemini api endpoints use model names in the URI path:
	// https://generativelanguage.googleapis.com/v1beta/models/{model}:embedContent?key={apiKey}
	url := fmt.Sprintf(
		"https://generativelanguage.googleapis.com/v1beta/models/%s:embedContent?key=%s",
		c.model,
		c.apiKey,
	)

	reqPayload := GeminiRequest{
		Content: GeminiContent{
			Parts: []GeminiPart{
				{Text: text},
			},
		},
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

	resp, err := c.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("gemini http request failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		var errResp GeminiResponse
		if err := json.NewDecoder(resp.Body).Decode(&errResp); err == nil && errResp.Error != nil {
			return nil, fmt.Errorf("gemini api error (HTTP %d - %s): %s", resp.StatusCode, errResp.Error.Status, errResp.Error.Message)
		}
		return nil, fmt.Errorf("gemini api returned status: %s", resp.Status)
	}

	var apiResp GeminiResponse
	if err := json.NewDecoder(resp.Body).Decode(&apiResp); err != nil {
		return nil, fmt.Errorf("failed to decode gemini response: %w", err)
	}

	if len(apiResp.Embedding.Values) == 0 {
		return nil, fmt.Errorf("gemini returned empty embedding data list")
	}

	return apiResp.Embedding.Values, nil
}

package modelrouter

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

// APIEmbedder calls an OpenAI-compatible embeddings endpoint. Tongyi's
// OpenAI-compatible endpoint uses the same request and response shape.
type APIEmbedder struct {
	Endpoint   string
	APIKey     string
	Model      string
	Dimensions int
	Client     *http.Client
}

func (e *APIEmbedder) Dimension() int { return e.Dimensions }

func (e *APIEmbedder) Embed(ctx context.Context, text string) ([]float64, error) {
	if strings.TrimSpace(e.Endpoint) == "" {
		return nil, fmt.Errorf("embedding API endpoint is required")
	}
	payload := map[string]any{"model": e.Model, "input": text}
	if e.Dimensions > 0 {
		payload["dimensions"] = e.Dimensions
	}
	body, err := json.Marshal(payload)
	if err != nil {
		return nil, fmt.Errorf("marshal embedding request: %w", err)
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, e.Endpoint, bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("create embedding request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	if e.APIKey != "" {
		req.Header.Set("Authorization", "Bearer "+e.APIKey)
	}
	client := e.Client
	if client == nil {
		client = &http.Client{Timeout: 15 * time.Second}
	}
	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("call embedding API: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		message, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
		return nil, fmt.Errorf("embedding API returned %s: %s", resp.Status, strings.TrimSpace(string(message)))
	}
	var result struct {
		Data []struct {
			Embedding []float64 `json:"embedding"`
		} `json:"data"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, fmt.Errorf("decode embedding response: %w", err)
	}
	if len(result.Data) == 0 || len(result.Data[0].Embedding) == 0 {
		return nil, fmt.Errorf("embedding API returned no vectors")
	}
	if e.Dimensions > 0 && len(result.Data[0].Embedding) != e.Dimensions {
		return nil, fmt.Errorf("embedding dimension mismatch: got %d, want %d", len(result.Data[0].Embedding), e.Dimensions)
	}
	return normalizeVector(result.Data[0].Embedding), nil
}

// FallbackEmbedder uses Primary first and falls back only when it fails.
type FallbackEmbedder struct {
	Primary  Embedder
	Fallback Embedder
}

func (e *FallbackEmbedder) Dimension() int {
	if e.Primary != nil {
		return e.Primary.Dimension()
	}
	if e.Fallback != nil {
		return e.Fallback.Dimension()
	}
	return 0
}

func (e *FallbackEmbedder) Embed(ctx context.Context, text string) ([]float64, error) {
	if e.Primary != nil {
		v, err := e.Primary.Embed(ctx, text)
		if err == nil {
			return v, nil
		}
		if e.Fallback == nil {
			return nil, err
		}
	}
	if e.Fallback == nil {
		return nil, fmt.Errorf("no embedding provider configured")
	}
	return e.Fallback.Embed(ctx, text)
}

package modelrouter

import (
	"context"
	"fmt"
	"math/rand"
	"os"
	"strconv"
	"strings"
	"sync"

	"github.com/modelbus/one-api-pro/model"
)

func init() {
	Register("embedding", func() ModelRouter { return &EmbeddingModelRouter{} })
}

type EmbeddingModelRouter struct {
	once   sync.Once
	scorer *EmbeddingScorer
	err    error
}

func (r *EmbeddingModelRouter) Name() string { return "embedding" }

func (r *EmbeddingModelRouter) SelectModel(ctx context.Context, group string, _ int, req *ModelSelectRequest) (string, error) {
	models, err := model.CacheGetGroupModels(ctx, group)
	if err != nil || len(models) == 0 {
		return "", fmt.Errorf("no available models for group %s", group)
	}
	models = filterModelsWithPricing(ctx, models)
	if len(models) == 0 {
		return "", fmt.Errorf("no models with pricing found for group %s", group)
	}
	if req == nil || len(req.Messages) == 0 {
		return models[rand.Intn(len(models))], nil
	}
	prompt := extractPrompt(req.Messages)
	if prompt == "" {
		return models[rand.Intn(len(models))], nil
	}
	r.once.Do(func() { r.scorer, r.err = newEmbeddingScorerFromEnv() })
	if r.err != nil {
		return "", r.err
	}
	scores, err := r.scorer.Score(ctx, prompt, models)
	if err != nil {
		return "", fmt.Errorf("semantic model scoring: %w", err)
	}
	best := models[0]
	bestScore := scores[best]
	for _, candidate := range models[1:] {
		if scores[candidate] > bestScore {
			best, bestScore = candidate, scores[candidate]
		}
	}
	return best, nil
}

func newEmbeddingScorerFromEnv() (*EmbeddingScorer, error) {
	artifactsPath := envOrDefault("ARTIFACTS_PATH", "./modelrouter/artifacts")
	artifacts, err := LoadArtifacts(artifactsPath)
	if err != nil {
		return nil, err
	}
	modelName := envOrDefault("EMBEDDING_MODEL", "jina-v2-code")
	dimension := len(artifacts.Centroids[0])
	api := &APIEmbedder{
		Endpoint: os.Getenv("EMBEDDING_API_ENDPOINT"), APIKey: os.Getenv("EMBEDDING_API_KEY"),
		Model: modelName, Dimensions: dimension,
	}
	var embedder Embedder
	switch strings.ToLower(envOrDefault("EMBEDDING_PROVIDER", "onnx")) {
	case "api":
		embedder = api
	case "onnx":
		tokenizer, tokenizerErr := NewHuggingFaceTokenizer(os.Getenv("EMBEDDING_TOKENIZER_PATH"))
		if tokenizerErr != nil {
			if api.Endpoint == "" {
				return nil, tokenizerErr
			}
			embedder = api
			break
		}
		local, localErr := NewONNXEmbedder(ONNXEmbedderConfig{
			ModelPath: os.Getenv("EMBEDDING_MODEL_PATH"), RuntimeLibrary: os.Getenv("ONNXRUNTIME_LIBRARY"),
			Model: modelName, Dimension: dimension, Tokenizer: tokenizer,
		})
		if localErr != nil {
			if api.Endpoint == "" {
				return nil, localErr
			}
			embedder = api
		} else if api.Endpoint != "" {
			embedder = &FallbackEmbedder{Primary: local, Fallback: api}
		} else {
			embedder = local
		}
	default:
		return nil, fmt.Errorf("unsupported EMBEDDING_PROVIDER; use onnx or api")
	}
	topP, err := strconv.Atoi(envOrDefault("CLUSTER_TOPP", "4"))
	if err != nil || topP <= 0 {
		return nil, fmt.Errorf("CLUSTER_TOPP must be a positive integer")
	}
	return NewEmbeddingScorer(embedder, artifacts, topP)
}

func envOrDefault(name, fallback string) string {
	if value := strings.TrimSpace(os.Getenv(name)); value != "" {
		return value
	}
	return fallback
}

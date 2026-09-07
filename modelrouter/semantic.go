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
		// Explicit paths remain supported, but are treated as download targets when
		// absent so optional advanced configuration cannot block first startup.
		modelPath := expandHomeDir(strings.TrimSpace(os.Getenv("EMBEDDING_MODEL_PATH")))
		tokenizerPath := expandHomeDir(strings.TrimSpace(os.Getenv("EMBEDDING_TOKENIZER_PATH")))
		if !isNonEmptyFile(modelPath) || !isNonEmptyFile(tokenizerPath) {
			downloader := NewModelDownloader(
				os.Getenv("EMBEDDING_CACHE_DIR"),
				os.Getenv("EMBEDDING_MODEL_BASE_URL"),
			)
			downloadedModel, downloadedTokenizer, dlErr := downloader.ResolveModelFilesTo(modelName, modelPath, tokenizerPath)
			if dlErr != nil {
				if api.Endpoint == "" {
					return nil, fmt.Errorf("auto-download embedding model: %w (set EMBEDDING_MODEL_PATH and EMBEDDING_TOKENIZER_PATH manually, or configure EMBEDDING_MODEL_BASE_URL)", dlErr)
				}
				embedder = api
				break
			}
			modelPath = downloadedModel
			tokenizerPath = downloadedTokenizer
		}
		tokenizer, tokenizerErr := NewHuggingFaceTokenizer(tokenizerPath)
		if tokenizerErr != nil {
			if api.Endpoint == "" {
				return nil, tokenizerErr
			}
			embedder = api
			break
		}
		local, localErr := NewONNXEmbedder(ONNXEmbedderConfig{
			ModelPath: modelPath, RuntimeLibrary: os.Getenv("ONNXRUNTIME_LIBRARY"),
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

func isNonEmptyFile(path string) bool {
	if strings.TrimSpace(path) == "" {
		return false
	}
	info, err := os.Stat(expandHomeDir(path))
	return err == nil && !info.IsDir() && info.Size() > 0
}

func envOrDefault(name, fallback string) string {
	if value := strings.TrimSpace(os.Getenv(name)); value != "" {
		return value
	}
	return fallback
}

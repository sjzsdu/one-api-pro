package modelrouter

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
)

type ModelMetadata struct {
	Cost    float64 `json:"cost"`
	Latency float64 `json:"latency"`
}

type Artifacts struct {
	Centroids    [][]float64
	QualityMeans map[string][]float64
	Rankings     map[string][]string
	Models       map[string]ModelMetadata
}

func LoadArtifacts(dir string) (*Artifacts, error) {
	a := &Artifacts{}
	files := []struct {
		name string
		dst  any
	}{
		{"centroids.json", &a.Centroids},
		{"quality_means.json", &a.QualityMeans},
		{"rankings.json", &a.Rankings},
		{"model_registry.json", &a.Models},
	}
	for _, f := range files {
		data, err := os.ReadFile(filepath.Join(dir, f.name))
		if err != nil {
			return nil, fmt.Errorf("read %s: %w", f.name, err)
		}
		if err := json.Unmarshal(data, f.dst); err != nil {
			return nil, fmt.Errorf("decode %s: %w", f.name, err)
		}
	}
	if len(a.Centroids) == 0 {
		return nil, fmt.Errorf("artifacts contain no centroids")
	}
	return a, nil
}

type EmbeddingScorer struct {
	Embedder      Embedder
	Clusters      *ClusterManager
	Artifacts     *Artifacts
	QualityWeight float64
	CostWeight    float64
	SpeedWeight   float64
}

func NewEmbeddingScorer(embedder Embedder, artifacts *Artifacts, topP int) (*EmbeddingScorer, error) {
	if embedder == nil || artifacts == nil || len(artifacts.Centroids) == 0 {
		return nil, fmt.Errorf("embedder and non-empty artifacts are required")
	}
	if embedder.Dimension() > 0 && len(artifacts.Centroids[0]) != embedder.Dimension() {
		return nil, fmt.Errorf("artifact dimension %d does not match embedder dimension %d", len(artifacts.Centroids[0]), embedder.Dimension())
	}
	return &EmbeddingScorer{
		Embedder: embedder, Clusters: &ClusterManager{Centroids: artifacts.Centroids, TopP: topP}, Artifacts: artifacts,
		QualityWeight: 1, CostWeight: .1, SpeedWeight: .1,
	}, nil
}

// Score embeds a prompt once and scores only models available to the caller.
func (s *EmbeddingScorer) Score(ctx context.Context, prompt string, models []string) (map[string]float64, error) {
	scores, _, err := s.ScoreWithMatches(ctx, prompt, models)
	return scores, err
}

// ScoreWithMatches returns the same scores used for production selection plus
// the matched semantic clusters for route explanations and the model quiz.
func (s *EmbeddingScorer) ScoreWithMatches(ctx context.Context, prompt string, models []string) (map[string]float64, []ClusterMatch, error) {
	embedding, err := s.Embedder.Embed(ctx, prompt)
	if err != nil {
		return nil, nil, err
	}
	matches := s.Clusters.MatchClusters(embedding)
	if len(matches) == 0 {
		return nil, nil, fmt.Errorf("embedding does not match artifact dimensions")
	}
	costs := make(map[string]*float64, len(models))
	latencies := make(map[string]*float64, len(models))
	for _, name := range models {
		if metadata, ok := s.Artifacts.Models[CanonicalModelName(name)]; ok {
			cost, latency := metadata.Cost, metadata.Latency
			costs[name], latencies[name] = &cost, &latency
		}
	}
	costScores := normalizeLowerIsBetter(models, costs)
	latencyScores := normalizeLowerIsBetter(models, latencies)
	scores := make(map[string]float64, len(models))
	for _, model := range models {
		canonicalName := CanonicalModelName(model)
		quality := .5
		if qualities, known := s.Artifacts.QualityMeans[canonicalName]; known {
			var weighted, weight float64
			for _, match := range matches {
				if match.Cluster < len(qualities) {
					w := max(0, match.Similarity)
					weighted += qualities[match.Cluster] * w
					weight += w
				}
			}
			if weight > 0 {
				quality = weighted / weight
			}
		}
		scores[model] = s.QualityWeight*quality + s.CostWeight*costScores[model] + s.SpeedWeight*latencyScores[model]
	}
	return scores, matches, nil
}

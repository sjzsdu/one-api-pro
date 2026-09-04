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
	embedding, err := s.Embedder.Embed(ctx, prompt)
	if err != nil {
		return nil, err
	}
	matches := s.Clusters.MatchClusters(embedding)
	if len(matches) == 0 {
		return nil, fmt.Errorf("embedding does not match artifact dimensions")
	}
	scores := make(map[string]float64, len(models))
	for _, model := range models {
		qualities := s.Artifacts.QualityMeans[model]
		var quality, weight float64
		for _, match := range matches {
			if match.Cluster < len(qualities) {
				w := match.Similarity
				if w < 0 {
					w = 0
				}
				quality += qualities[match.Cluster] * w
				weight += w
			}
		}
		if weight > 0 {
			quality /= weight
		}
		meta := s.Artifacts.Models[model]
		scores[model] = s.QualityWeight*quality - s.CostWeight*meta.Cost - s.SpeedWeight*meta.Latency
	}
	return scores, nil
}

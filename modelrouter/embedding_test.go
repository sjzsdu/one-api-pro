package modelrouter

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
	ort "github.com/yalue/onnxruntime_go"
)

type staticEmbedder []float64

func (e staticEmbedder) Embed(context.Context, string) ([]float64, error) { return e, nil }
func (e staticEmbedder) Dimension() int                                   { return len(e) }

func TestClusterManagerTopP(t *testing.T) {
	cm := ClusterManager{Centroids: [][]float64{{1, 0}, {0, 1}, {.8, .2}}, TopP: 2}
	matches := cm.MatchClusters([]float64{1, 0})
	require.Len(t, matches, 2)
	require.Equal(t, 0, matches[0].Cluster)
	require.Equal(t, 2, matches[1].Cluster)
}

func TestEmbeddingScorerUsesQualityCostAndLatency(t *testing.T) {
	artifacts := &Artifacts{
		Centroids:    [][]float64{{1, 0}},
		QualityMeans: map[string][]float64{"quality": {1}, "cheap": {.9}},
		Models:       map[string]ModelMetadata{"quality": {Cost: 10}, "cheap": {Cost: 0}},
	}
	scorer, err := NewEmbeddingScorer(staticEmbedder{1, 0}, artifacts, 1)
	require.NoError(t, err)
	scores, err := scorer.Score(context.Background(), "prompt", []string{"quality", "cheap"})
	require.NoError(t, err)
	require.Greater(t, scores["cheap"], scores["quality"])
}

func TestAPIEmbedderOpenAICompatibleResponse(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		require.Equal(t, "Bearer secret", r.Header.Get("Authorization"))
		_ = json.NewEncoder(w).Encode(map[string]any{"data": []any{map[string]any{"embedding": []float64{3, 4}}}})
	}))
	defer server.Close()
	embedder := APIEmbedder{Endpoint: server.URL, APIKey: "secret", Model: "test", Dimensions: 2}
	vector, err := embedder.Embed(context.Background(), "hello")
	require.NoError(t, err)
	require.InDelta(t, .6, vector[0], 1e-9)
	require.InDelta(t, .8, vector[1], 1e-9)
}

func TestWordPieceTokenizer(t *testing.T) {
	path := filepath.Join(t.TempDir(), "tokenizer.json")
	require.NoError(t, os.WriteFile(path, []byte(`{"model":{"vocab":{"[UNK]":0,"[CLS]":1,"[SEP]":2,"hello":3,"##world":4}}}`), 0o600))
	tokenizer, err := NewWordPieceTokenizer(path)
	require.NoError(t, err)
	ids, err := tokenizer.Encode("helloworld", 8)
	require.NoError(t, err)
	require.Equal(t, []int64{1, 3, 4, 2}, ids)
}

func TestPoolEmbeddingTruncatesMatryoshkaDimensions(t *testing.T) {
	data := []float32{1, 2, 100, 3, 4, 200}
	vector, err := poolEmbedding(data, ort.NewShape(1, 2, 3), 2, 2)
	require.NoError(t, err)
	require.Equal(t, []float64{2, 3}, vector)
}

func BenchmarkClusterMatch(b *testing.B) {
	centroids := make([][]float64, 64)
	for i := range centroids {
		centroids[i] = make([]float64, 768)
		centroids[i][i] = 1
	}
	cm := ClusterManager{Centroids: centroids, TopP: 4}
	embedding := make([]float64, 768)
	embedding[0] = 1
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		cm.MatchClusters(embedding)
	}
}

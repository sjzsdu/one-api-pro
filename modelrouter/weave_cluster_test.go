package modelrouter

import (
	"context"
	"encoding/binary"
	"math"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
)

type staticEmbedder []float64

func (e staticEmbedder) Embed(context.Context, string) ([]float64, error) { return e, nil }
func (e staticEmbedder) Dimension() int                                   { return len(e) }

func TestLoadWeaveCentroids(t *testing.T) {
	raw := make([]byte, 16+2*2*4)
	copy(raw[:4], "CRT1")
	binary.LittleEndian.PutUint32(raw[4:8], 1)
	binary.LittleEndian.PutUint32(raw[8:12], 2)
	binary.LittleEndian.PutUint32(raw[12:16], 2)
	values := []float32{1, 0, 0, 1}
	for i, value := range values {
		binary.LittleEndian.PutUint32(raw[16+4*i:], math.Float32bits(value))
	}
	centroids, err := loadWeaveCentroids(raw)
	require.NoError(t, err)
	require.Equal(t, [][]float64{{1, 0}, {0, 1}}, centroids)
}

func TestWeaveClusterScorerUsesOfflineQualityAndCost(t *testing.T) {
	cheap, expensive := .1, 1.0
	scorer := &WeaveClusterScorer{
		Embedder: staticEmbedder{1, 0},
		Artifacts: &weaveClusterArtifacts{
			Centroids: [][]float64{{1, 0}}, Models: []string{"quality", "cheap"},
			ModelSet: map[string]struct{}{"quality": {}, "cheap": {}},
			Quality:  map[int]map[string]float64{0: {"quality": 1, "cheap": .6}},
			Axes:     map[string]weaveModelAxis{"quality": {InputPer1KUSD: &expensive}, "cheap": {InputPer1KUSD: &cheap}},
			Alpha:    []float64{.8}, TopP: 1,
		},
		Clusters: &ClusterManager{Centroids: [][]float64{{1, 0}}, TopP: 1},
	}
	details, _, err := scorer.Score(context.Background(), "prove the algorithm is correct", []string{"quality", "cheap", "unknown"})
	require.NoError(t, err)
	require.Greater(t, details["quality"].Total, details["cheap"].Total)
	require.NotContains(t, details, "unknown")
}

func TestWeaveClusterScorerFavorsCostForTranslation(t *testing.T) {
	cheap, expensive := .1, 1.0
	scorer := &WeaveClusterScorer{
		Embedder: staticEmbedder{1, 0},
		Artifacts: &weaveClusterArtifacts{
			Centroids: [][]float64{{1, 0}}, Models: []string{"quality", "cheap"},
			ModelSet: map[string]struct{}{"quality": {}, "cheap": {}},
			Quality:  map[int]map[string]float64{0: {"quality": 1, "cheap": .6}},
			Axes:     map[string]weaveModelAxis{"quality": {InputPer1KUSD: &expensive}, "cheap": {InputPer1KUSD: &cheap}},
			Alpha:    []float64{.96}, TopP: 1,
		},
		Clusters: &ClusterManager{Centroids: [][]float64{{1, 0}}, TopP: 1},
	}
	details, _, err := scorer.Score(context.Background(), "请翻译：hello world", []string{"quality", "cheap"})
	require.NoError(t, err)
	require.Greater(t, details["cheap"].Total, details["quality"].Total)
	require.InDelta(t, .2, details["cheap"].Components["effective_quality_weight"], 1e-9)
}

func TestWeaveClusterScorerPreservesComplexRequestFeatures(t *testing.T) {
	cheap, expensive := .1, 1.0
	scorer := &WeaveClusterScorer{
		Embedder: staticEmbedder{1, 0},
		Artifacts: &weaveClusterArtifacts{
			Centroids: [][]float64{{1, 0}}, Models: []string{"quality", "cheap"},
			ModelSet: map[string]struct{}{"quality": {}, "cheap": {}},
			Quality:  map[int]map[string]float64{0: {"quality": 1, "cheap": .6}},
			Axes:     map[string]weaveModelAxis{"quality": {InputPer1KUSD: &expensive}, "cheap": {InputPer1KUSD: &cheap}},
			Alpha:    []float64{.2}, TopP: 1,
		},
		Clusters: &ClusterManager{Centroids: [][]float64{{1, 0}}, TopP: 1},
	}
	details, _, err := scorer.ScoreWithFeatures(context.Background(), "hello", []string{"quality", "cheap"}, &RequestFeatures{Prompt: "hello", HasTools: true, Difficulty: TaskDifficultyComplex})
	require.NoError(t, err)
	require.Greater(t, details["quality"].Total, details["cheap"].Total)
	require.InDelta(t, 1, details["quality"].Components["effective_quality_weight"], 1e-9)
}

func TestWeaveClusterScorerUsesCandidatePricesForGPT56Variants(t *testing.T) {
	artifactCost := .005
	cheapPrice, expensivePrice := .2, 20.0
	scorer := &WeaveClusterScorer{
		Embedder: staticEmbedder{1, 0},
		Artifacts: &weaveClusterArtifacts{
			Centroids: [][]float64{{1, 0}}, Models: []string{"gpt-5.6-sol", "gpt-5.6-luna"},
			ModelSet: map[string]struct{}{"gpt-5.6-sol": {}, "gpt-5.6-luna": {}},
			Quality:  map[int]map[string]float64{0: {"gpt-5.6-sol": 1, "gpt-5.6-luna": .8}},
			Axes: map[string]weaveModelAxis{
				"gpt-5.6-sol":  {InputPer1KUSD: &artifactCost},
				"gpt-5.6-luna": {InputPer1KUSD: &artifactCost},
			},
			Alpha: []float64{.96}, TopP: 1,
		},
		Clusters: &ClusterManager{Centroids: [][]float64{{1, 0}}, TopP: 1},
	}
	profiles := map[string]ModelProfile{
		"gpt-5.6-sol":  {InputCost: &expensivePrice},
		"gpt-5.6-luna": {InputCost: &cheapPrice},
	}
	candidates := []string{"gpt-5.6-sol", "gpt-5.6-luna"}

	details, _, err := scorer.ScoreWithCandidateProfiles(context.Background(), "hello", candidates, &RequestFeatures{Prompt: "hello", Difficulty: TaskDifficultySimple}, profiles)
	require.NoError(t, err)
	require.Greater(t, details["gpt-5.6-luna"].Total, details["gpt-5.6-sol"].Total)

	details, _, err = scorer.ScoreWithCandidateProfiles(context.Background(), "debug this algorithm", candidates, &RequestFeatures{Prompt: "debug this algorithm", Difficulty: TaskDifficultyComplex}, profiles)
	require.NoError(t, err)
	require.Greater(t, details["gpt-5.6-sol"].Total, details["gpt-5.6-luna"].Total)
}

func TestWeaveClusterScorerResolvesLatestAliasOnlyWhenArtifactKnowsBaseModel(t *testing.T) {
	scorer := &WeaveClusterScorer{Artifacts: &weaveClusterArtifacts{ModelSet: map[string]struct{}{"deepseek/deepseek-v4-flash": {}}}}
	require.Equal(t, "deepseek/deepseek-v4-flash", scorer.resolveArtifactModel("~deepseek/deepseek-v4-flash-latest"))
	require.Empty(t, scorer.resolveArtifactModel("gpt-5.6-sol"))
}

func TestWeaveClusterArtifactAppliesLocalOverride(t *testing.T) {
	path := filepath.Join(t.TempDir(), "overrides.json")
	require.NoError(t, os.WriteFile(path, []byte(`{"models":{"gpt-5.6-sol":{"copy_from":"gpt-5.5","quality_delta":0.09,"input_cost_multiplier":0.5,"output_cost_multiplier":0.25}}}`), 0o600))
	t.Setenv("CLUSTER_MODEL_OVERRIDES_PATH", path)
	inputCost, outputCost := .005, .04
	artifacts := &weaveClusterArtifacts{
		Centroids: [][]float64{{1}, {1}}, Models: []string{"gpt-5.5"},
		ModelSet: map[string]struct{}{"gpt-5.5": {}},
		Quality:  map[int]map[string]float64{0: {"gpt-5.5": .8}, 1: {"gpt-5.5": .9}},
		Axes:     map[string]weaveModelAxis{"gpt-5.5": {InputPer1KUSD: &inputCost, OutputPer1KUSD: &outputCost}},
	}
	require.NoError(t, artifacts.applyLocalOverrides())
	require.InDelta(t, .89, artifacts.Quality[0]["gpt-5.6-sol"], 1e-9)
	require.InDelta(t, .99, artifacts.Quality[1]["gpt-5.6-sol"], 1e-9)
	require.Contains(t, artifacts.ModelSet, "gpt-5.6-sol")
	require.InDelta(t, .0025, *artifacts.Axes["gpt-5.6-sol"].InputPer1KUSD, 1e-9)
	require.InDelta(t, .01, *artifacts.Axes["gpt-5.6-sol"].OutputPer1KUSD, 1e-9)
	require.InDelta(t, .005, *artifacts.Axes["gpt-5.5"].InputPer1KUSD, 1e-9)
}

func TestGPT56OverridesPreferLunaForSimpleRequestsWithoutCatalogPrices(t *testing.T) {
	path := filepath.Join(t.TempDir(), "overrides.json")
	require.NoError(t, os.WriteFile(path, []byte(`{"models":{"gpt-5.6-sol":{"copy_from":"gpt-5.5","quality_delta":0.09,"input_cost_multiplier":1},"gpt-5.6-terra":{"copy_from":"gpt-5.5","quality_delta":0.045,"input_cost_multiplier":0.5},"gpt-5.6-luna":{"copy_from":"gpt-5.5","quality_delta":0,"input_cost_multiplier":0.2}}}`), 0o600))
	t.Setenv("CLUSTER_MODEL_OVERRIDES_PATH", path)
	inputCost := .005
	artifacts := &weaveClusterArtifacts{
		Centroids: [][]float64{{1, 0}}, Models: []string{"gpt-5.5"},
		ModelSet: map[string]struct{}{"gpt-5.5": {}},
		Quality:  map[int]map[string]float64{0: {"gpt-5.5": .8}},
		Axes:     map[string]weaveModelAxis{"gpt-5.5": {InputPer1KUSD: &inputCost}},
		Alpha:    []float64{.96}, TopP: 1,
	}
	require.NoError(t, artifacts.applyLocalOverrides())
	scorer := &WeaveClusterScorer{
		Embedder: staticEmbedder{1, 0}, Artifacts: artifacts,
		Clusters: &ClusterManager{Centroids: artifacts.Centroids, TopP: 1},
	}
	candidates := []string{"gpt-5.6-sol", "gpt-5.6-terra", "gpt-5.6-luna"}

	details, _, err := scorer.Score(context.Background(), "hello", candidates)
	require.NoError(t, err)
	require.Greater(t, details["gpt-5.6-luna"].Total, details["gpt-5.6-terra"].Total)
	require.Greater(t, details["gpt-5.6-terra"].Total, details["gpt-5.6-sol"].Total)
}

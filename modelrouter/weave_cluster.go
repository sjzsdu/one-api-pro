package modelrouter

import (
	"context"
	"encoding/binary"
	"encoding/json"
	"fmt"
	"math"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"

	"gopkg.in/yaml.v3"
)

// The public Weave bundle is an offline-evaluated cluster router. Keeping the
// URL/version explicit makes upgrades reviewable and avoids treating the small
// JSON files once checked into this repository as production quality data.
const defaultWeaveArtifactBaseURL = "https://raw.githubusercontent.com/weave-os/router/main/internal/router/cluster/artifacts"

type weaveModelAxis struct {
	InputPer1KUSD  *float64 `json:"input_per_1k_usd"`
	OutputPer1KUSD *float64 `json:"output_per_1k_usd"`
	TTFTSeconds    *float64 `json:"ttft_s"`
	TPS            *float64 `json:"tps"`
}

type weaveModelFeaturesFile struct {
	Models map[string]struct {
		PsiProbe    []float64      `json:"psi_probe"`
		Operational weaveModelAxis `json:"operational"`
	} `json:"models"`
}

// clusterModelOverrides is an operator-owned overlay. It is intentionally
// separate from the downloaded bundle so an upstream artifact upgrade never
// overwrites local model aliases or calibrated priors.
type clusterModelOverrides struct {
	Models map[string]clusterModelOverride `json:"models"`
}

type clusterModelOverride struct {
	CopyFrom             string   `json:"copy_from"`
	QualityDelta         float64  `json:"quality_delta"`
	InputCostMultiplier  *float64 `json:"input_cost_multiplier,omitempty"`
	OutputCostMultiplier *float64 `json:"output_cost_multiplier,omitempty"`
	Note                 string   `json:"note,omitempty"`
}

type weaveRegistryFile struct {
	DeployedModels []struct {
		Model string `json:"model"`
	} `json:"deployed_models"`
}

type weaveMetadata struct {
	Version  string `yaml:"version"`
	Embedder struct {
		Model    string `yaml:"model"`
		EmbedDim int    `yaml:"embed_dim"`
	} `yaml:"embedder"`
	Training struct {
		K        int     `yaml:"k"`
		TopP     int     `yaml:"top_p"`
		Alpha    float64 `yaml:"alpha"`
		Defaults struct {
			Alpha                []float64 `yaml:"alpha"`
			SpeedWeight          float64   `yaml:"speed_weight"`
			OutputCostRatio      float64   `yaml:"output_cost_ratio"`
			ExpectedOutputTokens int       `yaml:"expected_output_tokens"`
		} `yaml:"default_routing_knobs"`
	} `yaml:"training"`
}

type weaveClusterArtifacts struct {
	Version              string
	Centroids            [][]float64
	Models               []string
	ModelSet             map[string]struct{}
	Quality              map[int]map[string]float64
	Axes                 map[string]weaveModelAxis
	Alpha                []float64
	TopP                 int
	SpeedWeight          float64
	OutputCostRatio      float64
	ExpectedOutputTokens int
}

// resolveWeaveArtifactDir downloads the immutable public bundle once into a
// user cache. Operators can set CLUSTER_ARTIFACT_DIR to use an internally
// reviewed mirror instead.
func resolveWeaveArtifactDir() (string, error) {
	if dir := expandHomeDir(strings.TrimSpace(os.Getenv("CLUSTER_ARTIFACT_DIR"))); dir != "" {
		return dir, nil
	}
	version := envOrDefault("CLUSTER_ARTIFACT_VERSION", "v0.75")
	cacheRoot := expandHomeDir(strings.TrimSpace(os.Getenv("CLUSTER_ARTIFACT_CACHE_DIR")))
	if cacheRoot == "" {
		home, err := os.UserHomeDir()
		if err != nil {
			return "", fmt.Errorf("resolve cluster artifact cache: %w", err)
		}
		cacheRoot = filepath.Join(home, ".one-api-pro", "cluster-artifacts")
	}
	dir := filepath.Join(cacheRoot, version)
	baseURL := strings.TrimRight(envOrDefault("CLUSTER_ARTIFACT_BASE_URL", defaultWeaveArtifactBaseURL), "/") + "/" + version
	downloader := NewModelDownloader(cacheRoot, "")
	for _, filename := range []string{"centroids.bin", "metadata.yaml", "model_registry.json", "model_features.json"} {
		path := filepath.Join(dir, filename)
		if err := os.MkdirAll(filepath.Dir(path), modelDirPermission); err != nil {
			return "", fmt.Errorf("create cluster artifact cache: %w", err)
		}
		if err := downloader.ensureFile(path, ModelFile{Filename: filename, URL: baseURL + "/" + filename}); err != nil {
			return "", fmt.Errorf("download cluster artifact %s: %w", filename, err)
		}
	}
	return dir, nil
}

func loadWeaveClusterArtifacts(dir string) (*weaveClusterArtifacts, error) {
	centroidRaw, err := os.ReadFile(filepath.Join(dir, "centroids.bin"))
	if err != nil {
		return nil, fmt.Errorf("read centroids.bin: %w", err)
	}
	centroids, err := loadWeaveCentroids(centroidRaw)
	if err != nil {
		return nil, err
	}
	metaRaw, err := os.ReadFile(filepath.Join(dir, "metadata.yaml"))
	if err != nil {
		return nil, fmt.Errorf("read metadata.yaml: %w", err)
	}
	var metadata weaveMetadata
	if err := yaml.Unmarshal(metaRaw, &metadata); err != nil {
		return nil, fmt.Errorf("parse metadata.yaml: %w", err)
	}
	if metadata.Embedder.EmbedDim != len(centroids[0]) {
		return nil, fmt.Errorf("artifact embedding dimension %d does not match centroids %d", metadata.Embedder.EmbedDim, len(centroids[0]))
	}
	registryRaw, err := os.ReadFile(filepath.Join(dir, "model_registry.json"))
	if err != nil {
		return nil, fmt.Errorf("read model_registry.json: %w", err)
	}
	var registry weaveRegistryFile
	if err := json.Unmarshal(registryRaw, &registry); err != nil {
		return nil, fmt.Errorf("parse model_registry.json: %w", err)
	}
	featuresRaw, err := os.ReadFile(filepath.Join(dir, "model_features.json"))
	if err != nil {
		return nil, fmt.Errorf("read model_features.json: %w", err)
	}
	var features weaveModelFeaturesFile
	if err := json.Unmarshal(featuresRaw, &features); err != nil {
		return nil, fmt.Errorf("parse model_features.json: %w", err)
	}
	if len(registry.DeployedModels) == 0 || len(features.Models) == 0 {
		return nil, fmt.Errorf("artifact contains no deployed model features")
	}

	artifact := &weaveClusterArtifacts{Version: metadata.Version, Centroids: centroids, ModelSet: make(map[string]struct{}),
		Quality: make(map[int]map[string]float64), Axes: make(map[string]weaveModelAxis),
		TopP: metadata.Training.TopP, SpeedWeight: metadata.Training.Defaults.SpeedWeight,
		OutputCostRatio: metadata.Training.Defaults.OutputCostRatio, ExpectedOutputTokens: metadata.Training.Defaults.ExpectedOutputTokens}
	if artifact.TopP <= 0 {
		artifact.TopP = 4
	}
	if len(metadata.Training.Defaults.Alpha) > 0 {
		artifact.Alpha = append([]float64(nil), metadata.Training.Defaults.Alpha...)
	} else {
		artifact.Alpha = make([]float64, len(centroids))
		for i := range artifact.Alpha {
			artifact.Alpha[i] = metadata.Training.Alpha
		}
	}
	if len(artifact.Alpha) != len(centroids) {
		return nil, fmt.Errorf("artifact alpha count %d does not match clusters %d", len(artifact.Alpha), len(centroids))
	}
	for _, entry := range registry.DeployedModels {
		name := strings.TrimSpace(entry.Model)
		feature, ok := features.Models[name]
		if name == "" || !ok || len(feature.PsiProbe) != len(centroids) {
			return nil, fmt.Errorf("artifact model %q has missing or incompatible features", name)
		}
		artifact.Models = append(artifact.Models, name)
		artifact.ModelSet[name] = struct{}{}
		artifact.Axes[name] = feature.Operational
		for cluster, quality := range feature.PsiProbe {
			if artifact.Quality[cluster] == nil {
				artifact.Quality[cluster] = make(map[string]float64)
			}
			artifact.Quality[cluster][name] = quality
		}
	}
	if err := artifact.applyLocalOverrides(); err != nil {
		return nil, err
	}
	return artifact, nil
}

func (a *weaveClusterArtifacts) applyLocalOverrides() error {
	path := expandHomeDir(envOrDefault("CLUSTER_MODEL_OVERRIDES_PATH", "./modelrouter/artifacts/cluster_model_overrides.json"))
	raw, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("read cluster model overrides: %w", err)
	}
	var overrides clusterModelOverrides
	if err := json.Unmarshal(raw, &overrides); err != nil {
		return fmt.Errorf("parse cluster model overrides: %w", err)
	}
	for target, override := range overrides.Models {
		target, source := strings.TrimSpace(target), strings.TrimSpace(override.CopyFrom)
		if target == "" || source == "" {
			return fmt.Errorf("cluster override requires a model name and copy_from")
		}
		if _, exists := a.ModelSet[target]; exists {
			return fmt.Errorf("cluster override %q duplicates an artifact model", target)
		}
		if _, exists := a.ModelSet[source]; !exists {
			return fmt.Errorf("cluster override %q references unknown copy_from model %q", target, source)
		}
		for cluster := range a.Centroids {
			a.Quality[cluster][target] = a.Quality[cluster][source] + override.QualityDelta
		}
		axis := a.Axes[source]
		axis.InputPer1KUSD = scaledCost(axis.InputPer1KUSD, override.InputCostMultiplier)
		axis.OutputPer1KUSD = scaledCost(axis.OutputPer1KUSD, override.OutputCostMultiplier)
		a.Axes[target] = axis
		a.Models = append(a.Models, target)
		a.ModelSet[target] = struct{}{}
	}
	return nil
}

func scaledCost(value, multiplier *float64) *float64 {
	if value == nil || multiplier == nil {
		return value
	}
	scaled := *value * max(0, *multiplier)
	return &scaled
}

func loadWeaveCentroids(raw []byte) ([][]float64, error) {
	if len(raw) < 16 || string(raw[:4]) != "CRT1" || binary.LittleEndian.Uint32(raw[4:8]) != 1 {
		return nil, fmt.Errorf("invalid Weave centroids.bin header")
	}
	k, dim := int(binary.LittleEndian.Uint32(raw[8:12])), int(binary.LittleEndian.Uint32(raw[12:16]))
	if k <= 0 || dim <= 0 || len(raw) != 16+4*k*dim {
		return nil, fmt.Errorf("invalid Weave centroids.bin dimensions")
	}
	centroids := make([][]float64, k)
	for i := range centroids {
		centroids[i] = make([]float64, dim)
		for j := range centroids[i] {
			centroids[i][j] = float64(math.Float32frombits(binary.LittleEndian.Uint32(raw[16+4*(i*dim+j):])))
		}
	}
	return centroids, nil
}

type WeaveClusterScorer struct {
	Embedder  Embedder
	Artifacts *weaveClusterArtifacts
	Clusters  *ClusterManager
}

func newWeaveClusterScorerFromEnv() (*WeaveClusterScorer, error) {
	dir, err := resolveWeaveArtifactDir()
	if err != nil {
		return nil, err
	}
	artifacts, err := loadWeaveClusterArtifacts(dir)
	if err != nil {
		return nil, err
	}
	// The current local ONNX setup supports the same Jina v2 embedding family
	// recorded by the public bundle. The scorer checks the returned dimension.
	modelName := envOrDefault("EMBEDDING_MODEL", "jina-v2-code")
	api := &APIEmbedder{Endpoint: os.Getenv("EMBEDDING_API_ENDPOINT"), APIKey: os.Getenv("EMBEDDING_API_KEY"), Model: modelName, Dimensions: len(artifacts.Centroids[0])}
	var embedder Embedder
	if strings.EqualFold(envOrDefault("EMBEDDING_PROVIDER", "onnx"), "api") {
		embedder = api
	} else {
		modelPath, tokenizerPath := expandHomeDir(strings.TrimSpace(os.Getenv("EMBEDDING_MODEL_PATH"))), expandHomeDir(strings.TrimSpace(os.Getenv("EMBEDDING_TOKENIZER_PATH")))
		if !isNonEmptyFile(modelPath) || !isNonEmptyFile(tokenizerPath) {
			modelPath, tokenizerPath, err = NewModelDownloader(os.Getenv("EMBEDDING_CACHE_DIR"), os.Getenv("EMBEDDING_MODEL_BASE_URL")).ResolveModelFilesTo(modelName, modelPath, tokenizerPath)
			if err != nil {
				return nil, fmt.Errorf("download cluster embedder: %w", err)
			}
		}
		tokenizer, err := NewHuggingFaceTokenizer(tokenizerPath)
		if err != nil {
			return nil, err
		}
		embedder, err = NewONNXEmbedder(ONNXEmbedderConfig{ModelPath: modelPath, RuntimeLibrary: os.Getenv("ONNXRUNTIME_LIBRARY"), Model: modelName, Dimension: len(artifacts.Centroids[0]), Tokenizer: tokenizer, MaxTokens: 256})
		if err != nil {
			return nil, err
		}
	}
	return &WeaveClusterScorer{Embedder: embedder, Artifacts: artifacts, Clusters: &ClusterManager{Centroids: artifacts.Centroids, TopP: artifacts.TopP}}, nil
}

func (s *WeaveClusterScorer) Score(ctx context.Context, prompt string, candidates []string) (map[string]CandidateScore, []ClusterMatch, error) {
	return s.score(ctx, prompt, candidates, nil, nil)
}

// ScoreWithFeatures ranks candidates while preserving the request capabilities
// already extracted by the caller (tools, context size, images, and so on).
func (s *WeaveClusterScorer) ScoreWithFeatures(ctx context.Context, prompt string, candidates []string, features *RequestFeatures) (map[string]CandidateScore, []ClusterMatch, error) {
	return s.score(ctx, prompt, candidates, features, nil)
}

// ScoreWithCandidateProfiles applies current catalog prices to the offline
// quality ranking. This is required for aliases whose artifact data was copied
// from the same base model but whose actual serving prices differ.
// 版本: v0.0.16
// 日期: 2026-09-11
func (s *WeaveClusterScorer) ScoreWithCandidateProfiles(ctx context.Context, prompt string, candidates []string, features *RequestFeatures, profiles map[string]ModelProfile) (map[string]CandidateScore, []ClusterMatch, error) {
	return s.score(ctx, prompt, candidates, features, profiles)
}

func (s *WeaveClusterScorer) score(ctx context.Context, prompt string, candidates []string, features *RequestFeatures, profiles map[string]ModelProfile) (map[string]CandidateScore, []ClusterMatch, error) {
	vector, err := s.Embedder.Embed(ctx, prompt)
	if err != nil {
		return nil, nil, err
	}
	matches := s.Clusters.MatchClusters(vector)
	if len(matches) == 0 {
		return nil, nil, fmt.Errorf("embedding dimension does not match cluster artifact")
	}
	artifactForCandidate := make(map[string]string)
	eligibleArtifactModels := make([]string, 0, len(candidates))
	seenArtifactModel := make(map[string]struct{})
	for _, candidate := range candidates {
		if name := s.resolveArtifactModel(candidate); name != "" {
			artifactForCandidate[candidate] = name
			if _, seen := seenArtifactModel[name]; !seen {
				seenArtifactModel[name] = struct{}{}
				eligibleArtifactModels = append(eligibleArtifactModels, name)
			}
		}
	}
	if len(artifactForCandidate) == 0 {
		return map[string]CandidateScore{}, matches, nil
	}
	qMin, qMax := make(map[int]float64), make(map[int]float64)
	for _, match := range matches {
		values := s.Artifacts.Quality[match.Cluster]
		qMin[match.Cluster], qMax[match.Cluster] = math.Inf(1), math.Inf(-1)
		for _, name := range eligibleArtifactModels {
			qMin[match.Cluster] = math.Min(qMin[match.Cluster], values[name])
			qMax[match.Cluster] = math.Max(qMax[match.Cluster], values[name])
		}
	}
	costs := make(map[string]float64)
	minCost, maxCost := math.Inf(1), math.Inf(-1)
	for candidate, name := range artifactForCandidate {
		axis := s.Artifacts.Axes[name]
		value := 0.0
		if axis.InputPer1KUSD != nil {
			value += *axis.InputPer1KUSD
		}
		if axis.OutputPer1KUSD != nil {
			value += s.Artifacts.OutputCostRatio * *axis.OutputPer1KUSD
		}
		if profile, ok := profiles[candidate]; ok && (profile.InputCost != nil || profile.OutputCost != nil) {
			// ModelPrice is stored as USD per million tokens while the artifact
			// axis is USD per thousand tokens. Keep both sources on one scale.
			value = 0
			if profile.InputCost != nil {
				value += *profile.InputCost / 1000
			}
			if profile.OutputCost != nil {
				value += s.Artifacts.OutputCostRatio * *profile.OutputCost / 1000
			}
		}
		costs[candidate] = value
		minCost, maxCost = math.Min(minCost, value), math.Max(maxCost, value)
	}
	details := make(map[string]CandidateScore, len(artifactForCandidate))
	alphaCap, difficulty := clusterQualityBudgetForFeatures(prompt, features)
	for candidate, name := range artifactForCandidate {
		total, qualityContribution, costContribution := 0.0, 0.0, 0.0
		effectiveAlpha := 0.0
		for _, match := range matches {
			alpha := math.Min(s.Artifacts.Alpha[match.Cluster], alphaCap)
			// Complex coding and tool-driven turns are explicitly quality-first.
			// The offline artifact's alpha is often calibrated for generic traffic,
			// so a low value here would otherwise make cost dominate every turn.
			if difficulty == TaskDifficultyComplex {
				alpha = alphaCap
			}
			effectiveAlpha += alpha
			q := 0.0
			if span := qMax[match.Cluster] - qMin[match.Cluster]; span > 0 {
				q = (s.Artifacts.Quality[match.Cluster][name] - qMin[match.Cluster]) / span
			}
			c := 0.0
			if span := maxCost - minCost; span > 0 {
				c = (costs[candidate] - minCost) / span
			}
			qualityContribution += alpha * q
			costContribution += (1 - alpha) * (1 - c)
			total += alpha*q + (1-alpha)*(1-c)
		}
		details[candidate] = CandidateScore{Total: total, Confidence: 1, Components: map[string]float64{
			"offline_cluster_score":    total,
			"quality_contribution":     qualityContribution,
			"cost_contribution":        costContribution,
			"matched_clusters":         float64(len(matches)),
			"effective_quality_weight": effectiveAlpha / float64(len(matches)),
			"task_difficulty":          clusterDifficultyScore(difficulty),
		}}
	}
	return details, matches, nil
}

// clusterQualityBudget applies the product-level policy missing from the
// upstream artifact: its default alpha is intentionally quality-heavy, while
// an API gateway must actively save money on low-risk requests. The cap does
// not alter the offline quality evidence; it only determines how much that
// evidence may outweigh cost for this request.
func clusterQualityBudget(prompt string) (float64, TaskDifficulty) {
	return clusterQualityBudgetForFeatures(prompt, nil)
}

func clusterQualityBudgetForFeatures(prompt string, features *RequestFeatures) (float64, TaskDifficulty) {
	if features == nil {
		features = &RequestFeatures{Prompt: prompt, Category: detectTaskCategory(prompt)}
		features.Difficulty = DetectTaskDifficulty(features)
	}
	// A short user utterance can accompany a substantial Codex system prompt
	// and tool set. Preserve explicit complex features before cheap-turn rules.
	if features.Difficulty == TaskDifficultyComplex {
		return 1, features.Difficulty
	}
	text := strings.ToLower(prompt)
	if features.Category == "translate" || features.Category == "chat" ||
		containsAny(text, "summarize", "summary", "rewrite", "shorten", "总结", "摘要", "改写", "润色") {
		return clusterAlphaFromEnv("CLUSTER_SIMPLE_MAX_ALPHA", .20), TaskDifficultySimple
	}
	switch features.Difficulty {
	case TaskDifficultySimple:
		return clusterAlphaFromEnv("CLUSTER_SIMPLE_MAX_ALPHA", .20), features.Difficulty
	case TaskDifficultyNormal:
		return clusterAlphaFromEnv("CLUSTER_NORMAL_MAX_ALPHA", .65), features.Difficulty
	default:
		return 1, TaskDifficultyComplex
	}
}

func clusterAlphaFromEnv(name string, fallback float64) float64 {
	value, err := strconv.ParseFloat(strings.TrimSpace(os.Getenv(name)), 64)
	if err != nil || math.IsNaN(value) || math.IsInf(value, 0) {
		return fallback
	}
	return max(0, min(1, value))
}

func clusterDifficultyScore(difficulty TaskDifficulty) float64 {
	switch difficulty {
	case TaskDifficultySimple:
		return 0
	case TaskDifficultyComplex:
		return 1
	default:
		return .5
	}
}

func (s *WeaveClusterScorer) resolveArtifactModel(candidate string) string {
	name := strings.TrimPrefix(strings.TrimSpace(candidate), "~")
	if _, ok := s.Artifacts.ModelSet[name]; ok {
		return name
	}
	if strings.HasSuffix(name, "-latest") {
		name = strings.TrimSuffix(name, "-latest")
		if _, ok := s.Artifacts.ModelSet[name]; ok {
			return name
		}
	}
	return ""
}

// Keep sort imported on old Go toolchains where map iteration in tests makes
// the registry order assertion explicit.
var _ = sort.Strings

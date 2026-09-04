package modelrouter

import (
	"math"
	"sort"
)

type ClusterMatch struct {
	Cluster    int     `json:"cluster"`
	Similarity float64 `json:"similarity"`
}

type ClusterManager struct {
	Centroids [][]float64
	TopP      int
}

// MatchClusters returns the closest centroids in descending cosine similarity.
func (cm *ClusterManager) MatchClusters(embedding []float64) []ClusterMatch {
	if len(embedding) == 0 || len(cm.Centroids) == 0 {
		return nil
	}
	matches := make([]ClusterMatch, 0, len(cm.Centroids))
	for i, centroid := range cm.Centroids {
		if len(centroid) != len(embedding) {
			continue
		}
		matches = append(matches, ClusterMatch{Cluster: i, Similarity: cosineSimilarity(embedding, centroid)})
	}
	sort.SliceStable(matches, func(i, j int) bool { return matches[i].Similarity > matches[j].Similarity })
	topP := cm.TopP
	if topP <= 0 || topP > len(matches) {
		topP = len(matches)
	}
	return matches[:topP]
}

func cosineSimilarity(a, b []float64) float64 {
	var dot, aa, bb float64
	for i := range a {
		dot += a[i] * b[i]
		aa += a[i] * a[i]
		bb += b[i] * b[i]
	}
	if aa == 0 || bb == 0 {
		return 0
	}
	return dot / math.Sqrt(aa*bb)
}

func normalizeVector(v []float64) []float64 {
	out := append([]float64(nil), v...)
	var sum float64
	for _, x := range out {
		sum += x * x
	}
	if sum == 0 {
		return out
	}
	norm := math.Sqrt(sum)
	for i := range out {
		out[i] /= norm
	}
	return out
}

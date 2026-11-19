// helper_test.go 中继模式路径映射单测
// 版本: v0.0.28
// 日期: 2026-11-19
// 作者: opencode
package relaymode

import "testing"

func TestGetByPathMultimodalEmbeddings(t *testing.T) {
	cases := []struct {
		path string
		want int
	}{
		{"/v1/multimodal/embeddings", MultimodalEmbeddings},
		{"/v1/multimodal/embeddings/", MultimodalEmbeddings},
		// The two existing routes must not regress.
		{"/v1/embeddings", Embeddings},
		{"/v1/engines/text-embedding-v3/embeddings", Embeddings},
		{"/v1/chat/completions", ChatCompletions},
	}
	for _, tc := range cases {
		t.Run(tc.path, func(t *testing.T) {
			if got := GetByPath(tc.path); got != tc.want {
				t.Fatalf("GetByPath(%q) = %d, want %d", tc.path, got, tc.want)
			}
		})
	}
}

func TestMultimodalEmbeddingsAppendedToIota(t *testing.T) {
	if MultimodalEmbeddings != AudioTranslation+1 {
		t.Fatalf("MultimodalEmbeddings = %d, want %d (must be appended, not inserted)",
			MultimodalEmbeddings, AudioTranslation+1)
	}
}

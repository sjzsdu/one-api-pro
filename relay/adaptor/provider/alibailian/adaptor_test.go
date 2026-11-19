// adaptor_test.go 百炼原生多模态向量 URL 构造单测
// 版本: v0.0.28
// 日期: 2026-11-19
// 作者: opencode
package alibailian

import (
	"testing"

	"github.com/modelbus/one-api-pro/relay/meta"
)

func TestGetMultimodalEmbeddingURL(t *testing.T) {
	const want = "https://dashscope.aliyuncs.com/api/v1/services/embeddings/multimodal-embedding"
	cases := []struct {
		name string
		base string
	}{
		{"bare domain", "https://dashscope.aliyuncs.com"},
		{"compatible-mode suffix", "https://dashscope.aliyuncs.com/compatible-mode"},
		{"compatible-mode with trailing slash", "https://dashscope.aliyuncs.com/compatible-mode/"},
		{"bare domain with trailing slash", "https://dashscope.aliyuncs.com/"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			a := &Adaptor{}
			got, err := a.GetMultimodalEmbeddingURL(&meta.Meta{BaseURL: tc.base})
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if got != want {
				t.Fatalf("base=%q got %q, want %q", tc.base, got, want)
			}
		})
	}
}

func TestGetMultimodalEmbeddingURLEmptyBase(t *testing.T) {
	a := &Adaptor{}
	if _, err := a.GetMultimodalEmbeddingURL(&meta.Meta{}); err == nil {
		t.Fatal("expected error for empty base URL")
	}
}

// model_family_test.go 已实现协议族的日期版本边界回归
// Rejects arbitrary suffixes while retaining dated native model variants.
// 版本: v0.0.28
// 日期: 2026-11-23
// 作者: Codex
package model

import "testing"

func TestAliOperationModelFamily_DatedKnownProtocols(t *testing.T) {
	for _, name := range []string{"qwen3-asr-flash-2025-09-08", "gte-rerank-v2-20250908", "wan2.6-t2v-2025-09-08", "wan2.7-image-pro-2025-09-08"} {
		if AliOperationModelFamily(name) == name {
			t.Fatalf("date version was not recognized: %s", name)
		}
		cap := ResolveModelCapabilities("ali", name, nil)
		if cap.Source != "builtin" || len(cap.Operations) != 1 && len(cap.Operations) != 2 {
			t.Fatalf("wrong capabilities: %s %+v", name, cap)
		}
	}
	for _, name := range []string{"qwen3-asr-flash-custom", "qwen3-asr-flash-2025-99-99", "wan2.7-image-unrelated", "gte-rerank-v2-20250908-custom"} {
		if AliOperationModelFamily(name) != name {
			t.Fatalf("arbitrary protocol suffix accepted %s", name)
		}
	}
}

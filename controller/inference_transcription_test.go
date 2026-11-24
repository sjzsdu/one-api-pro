// inference_transcription_test.go 原有ASR路由兼容性回归
// Selects native transcription by implemented operation rather than provider alone.
// 版本: v0.0.28
// 日期: 2026-11-23
// 作者: Codex
package controller

import "testing"

func TestTypedTranscription_OnlyImplementedNativeModels(t *testing.T) {
	for _, tc := range []struct {
		provider, name string
		want           bool
	}{
		{"ali", "qwen3-asr-flash", true}, {"ali", "qwen3-asr-flash-2025-09-08", true},
		{"ali", "legacy-asr", false}, {"ali", "paraformer-v2", false}, {"openai", "whisper-1", false},
	} {
		if got := typedTranscriptionModel(tc.provider, tc.name); got != tc.want {
			t.Fatalf("%s/%s got %v", tc.provider, tc.name, got)
		}
	}
}

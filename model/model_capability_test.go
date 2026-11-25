// model_capability_test.go 模型能力来源与操作边界回归测试
// Verifies catalog precedence and operation-specific execution constraints.
// 版本: v0.0.28
// 日期: 2026-11-23
// 作者: Codex
package model

import (
	"slices"
	"testing"
)

func TestResolveModelCapabilities_MetadataOverridesName(t *testing.T) {
	catalog := &ModelCatalog{Capabilities: `{"primary_category":"text","operations":{"chat.generate":{"modes":["sync"],"input_modalities":["text","image"],"output_modalities":["text"]}}}`}
	got := ResolveModelCapabilities("custom", "custom-image-chat", catalog)
	if got.PrimaryCategory != "text" || got.Source != "catalog" || len(got.Operations) != 1 {
		t.Fatalf("wrong capabilities: %+v", got)
	}
	catalog.CapabilityOverride = `{"primary_category":"audio","operations":{"audio.speech":{"modes":["sync"],"limits":{"formats":["wav"]}}}}`
	got = ResolveModelCapabilities("custom", "custom-image-chat", catalog)
	if got.PrimaryCategory != "audio" || got.Source != "override" || len(got.Operations) != 1 {
		t.Fatalf("override was merged with stale operations: %+v", got)
	}
}

func TestResolveModelCapabilities_OperationModesStayBound(t *testing.T) {
	got := ResolveModelCapabilities("ali", "omni-custom", &ModelCatalog{Capabilities: `{"primary_category":"audio","operations":{"chat.generate":{"modes":["realtime"]},"audio.speech":{"modes":["sync"],"limits":{"formats":["pcm"]}}}}`})
	if got.Operations["chat.generate"].Modes[0] != "realtime" || got.Operations["audio.speech"].Modes[0] != "sync" || got.Operations["audio.speech"].Limits.Formats[0] != "pcm" {
		t.Fatalf("operation boundaries lost: %+v", got)
	}
}

func TestResolveModelCapabilities_UnknownAndLegacyNames(t *testing.T) {
	if got := ResolveModelCapabilities("custom", "my-private-model", nil); got.PrimaryCategory != "unknown" || len(got.Operations) != 0 {
		t.Fatalf("unknown claimed chat support: %+v", got)
	}
	for _, name := range []string{"cogview-3", "wanx-v1"} {
		if got := ResolveModelCapabilities("custom", name, nil); got.PrimaryCategory != "image" || got.Source != "name_inference" {
			t.Fatalf("%s: %+v", name, got)
		}
	}
}

// TestBuiltinModelCapabilities_TTSVoiceWhitelist 锁定两个 TTS 模型的音色白名单：
// cosyvoice-v3 用 _v3 后缀，qwen-audio-3.1-tts 用 _v3.1 后缀，混用会被上游拒绝
// （表现为 "[cosyvoice:]Engine error [411]"），因此必须在能力目录里声明清楚。
func TestBuiltinModelCapabilities_TTSVoiceWhitelist(t *testing.T) {
	cosy := builtinModelCapabilities("ali", "cosyvoice-v3-flash")
	speech, ok := cosy.Operations["audio.speech"]
	if !ok {
		t.Fatal("cosyvoice-v3-flash should declare audio.speech")
	}
	for _, voice := range []string{"longanhuan_v3", "longwan_v3", "longfei_v3"} {
		if !slices.Contains(speech.Limits.Voices, voice) {
			t.Fatalf("cosyvoice voices missing %s: %v", voice, speech.Limits.Voices)
		}
	}
	if slices.Contains(speech.Limits.Voices, "longyu_v3") {
		t.Fatalf("cosyvoice voices must not contain the rejected longyu_v3: %v", speech.Limits.Voices)
	}

	qwen := builtinModelCapabilities("ali", "qwen-audio-3.1-tts-flash")
	qSpeech, ok := qwen.Operations["audio.speech"]
	if !ok {
		t.Fatal("qwen-audio-3.1-tts-flash should declare audio.speech")
	}
	for _, voice := range []string{"longanhuan_v3.1", "longwan_v3.1", "longfei_v3.1"} {
		if !slices.Contains(qSpeech.Limits.Voices, voice) {
			t.Fatalf("qwen-audio voices missing %s: %v", voice, qSpeech.Limits.Voices)
		}
	}
	if slices.Contains(qSpeech.Limits.Voices, "longanhuan_v3") {
		t.Fatalf("qwen-audio voices must not contain the cosyvoice-only longanhuan_v3: %v", qSpeech.Limits.Voices)
	}
}

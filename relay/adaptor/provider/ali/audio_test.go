// audio_test.go 百炼语音与图片参数回归测试
// Tests native media request conversion.
// 版本: v0.0.28
// 日期: 2026-11-23
// 作者: Codex
package ali

import (
	"testing"

	relaymodel "github.com/modelbus/one-api-pro/relay/schema"
)

// TestConvertAudioSpeechRequest verifies the gateway media contract.
// 版本: v0.0.28
// 日期: 2026-11-23
func TestConvertAudioSpeechRequest(t *testing.T) {
	a := &Adaptor{}

	t.Run("wav default and 24k sample rate", func(t *testing.T) {
		got, err := a.ConvertAudioSpeechRequest(&relaymodel.AudioSpeechRequest{
			Model:          "cosyvoice-v3-flash",
			Input:          "股市有风险，投资需谨慎",
			Voice:          "longanhuan_v3",
			ResponseFormat: "wav",
		})
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		native, ok := got.(*NativeTTSRequest)
		if !ok {
			t.Fatalf("expected *NativeTTSRequest, got %T", got)
		}
		if native.Input.Text != "股市有风险，投资需谨慎" {
			t.Errorf("text mismatch: %q", native.Input.Text)
		}
		if native.Input.Voice != "longanhuan_v3" {
			t.Errorf("voice mismatch: %q", native.Input.Voice)
		}
		if native.Input.Format != "wav" {
			t.Errorf("format mismatch: got %q want wav", native.Input.Format)
		}
		if native.Input.SampleRate != 24000 {
			t.Errorf("sample_rate mismatch: got %d want 24000", native.Input.SampleRate)
		}
		if native.Input.Rate != nil {
			t.Errorf("rate should be absent when speed is unset, got %v", native.Input.Rate)
		}
	})

	t.Run("mp3 mapping", func(t *testing.T) {
		got, _ := a.ConvertAudioSpeechRequest(&relaymodel.AudioSpeechRequest{
			Model: "cosyvoice-v3-flash", Input: "你好", Voice: "longsanshu_v3", ResponseFormat: "mp3",
		})
		if got.(*NativeTTSRequest).Input.Format != "mp3" {
			t.Errorf("mp3 mapping failed: %q", got.(*NativeTTSRequest).Input.Format)
		}
	})

	t.Run("nil and empty rejected", func(t *testing.T) {
		if _, err := a.ConvertAudioSpeechRequest(nil); err == nil {
			t.Error("nil request should error")
		}
		if _, err := a.ConvertAudioSpeechRequest(&relaymodel.AudioSpeechRequest{}); err == nil {
			t.Error("empty input should error")
		}
	})
}

// TestConvertImageRequestPassesNewFields verifies the gateway media contract.
// 版本: v0.0.28
// 日期: 2026-11-23
func TestConvertImageRequestPassesNewFields(t *testing.T) {
	seed := int64(42)
	got := ConvertImageRequest(relaymodel.ImageRequest{
		Model:          "wanx-v1",
		Prompt:         "电影感氛围背景摄影，深色高级质感",
		Size:           "1080x1440",
		Seed:           &seed,
		NegativePrompt: "不要文字",
		N:              1,
	})
	if got.Parameters.Size != "1080*1440" {
		t.Errorf("size x->* conversion failed: %q", got.Parameters.Size)
	}
	if got.Parameters.Seed == nil || *got.Parameters.Seed != 42 {
		t.Errorf("seed not passed through: %v", got.Parameters.Seed)
	}
	if got.Input.NegativePrompt != "不要文字" {
		t.Errorf("negative_prompt not passed through: %q", got.Input.NegativePrompt)
	}
}

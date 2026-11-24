// protocol.go 有界的 Qwen Omni WebSocket 协议
// Validates the supported Qwen manual-session subset without rewriting tool or media events.
// 版本: v0.0.28
// 日期: 2026-11-23
// 作者: Codex
package session

import (
	"encoding/base64"
	"encoding/json"
	"errors"
	"github.com/modelbus/one-api-pro/model"
)

const (
	// MaxInputTokens bounds prepaid input for each response, including conversation history.
	MaxInputTokens = 262144
	// MaxOutputTokens bounds prepaid output even if upstream truncation semantics change.
	MaxOutputTokens = 65536
)

// SupportedModel identifies the explicitly implemented Qwen 3.5 Omni protocol family.
// 版本: v0.0.28
// 日期: 2026-11-23
func SupportedModel(name string) bool {
	family := model.AliOperationModelFamily(name)
	return family == "qwen3.5-omni-flash-realtime" || family == "qwen3.5-omni-plus-realtime"
}

// ValidateClientEvent allows bounded manual Qwen events and preserves their original wire bytes.
// Automatic VAD responses and server-executed paid tools require a separate reservation policy.
// 版本: v0.0.28
// 日期: 2026-11-23
func ValidateClientEvent(data []byte, model string, maxTokens int) (string, error) {
	var event map[string]json.RawMessage
	if json.Unmarshal(data, &event) != nil {
		return "", errors.New("invalid realtime JSON event")
	}
	var kind string
	if json.Unmarshal(event["type"], &kind) != nil {
		return "", errors.New("missing realtime event type")
	}
	switch kind {
	case "session.update":
		var config map[string]json.RawMessage
		if json.Unmarshal(event["session"], &config) != nil || config == nil {
			return "", errors.New("session configuration is required")
		}
		allowed := map[string]bool{"model": true, "modalities": true, "voice": true, "audio": true, "input_audio_format": true, "output_audio_format": true, "instructions": true, "turn_detection": true, "tools": true, "temperature": true, "top_p": true, "top_k": true, "max_tokens": true, "repetition_penalty": true, "presence_penalty": true, "seed": true, "enable_search": true, "input_audio_transcription": true}
		for key := range config {
			if !allowed[key] {
				return "", errors.New("unsupported Qwen session setting")
			}
		}
		if raw, ok := config["model"]; ok {
			var pinned string
			if json.Unmarshal(raw, &pinned) != nil || pinned != model {
				return "", errors.New("session model is pinned to the selected channel")
			}
		}
		if raw, ok := config["turn_detection"]; ok && string(raw) != "null" {
			return "", errors.New("this gateway supports manual Qwen sessions only; turn_detection must be null")
		}
		if raw, ok := config["input_audio_transcription"]; ok && string(raw) != "null" {
			return "", errors.New("additional transcription is not enabled for this session")
		}
		if raw, ok := config["max_tokens"]; ok {
			var n int
			if json.Unmarshal(raw, &n) != nil || n < 1 || n > maxTokens {
				return "", errors.New("max_tokens exceeds the session output limit")
			}
		}
		if raw, ok := config["enable_search"]; ok && string(raw) != "false" {
			return "", errors.New("paid server-side search is not enabled for this session")
		}
		if raw, ok := config["voice"]; ok && !validVoice(raw) {
			return "", errors.New("supported Qwen voices are Tina and Ethan")
		}
		if raw, ok := config["modalities"]; ok {
			var modes []string
			if json.Unmarshal(raw, &modes) != nil || len(modes) == 0 || len(modes) > 2 {
				return "", errors.New("invalid output modalities")
			}
			for _, mode := range modes {
				if mode != "text" && mode != "audio" {
					return "", errors.New("supported output modalities are text and audio")
				}
			}
		}
		for _, field := range []string{"input_audio_format", "output_audio_format"} {
			if raw, ok := config[field]; ok {
				var f string
				if json.Unmarshal(raw, &f) != nil || (f != "pcm" && f != "pcm16" && f != "wav") {
					return "", errors.New("unsupported audio format")
				}
			}
		}
		if raw, ok := config["audio"]; ok {
			if err := validateAudio(raw); err != nil {
				return "", err
			}
		}
		if raw, ok := config["tools"]; ok {
			var tools []struct {
				Type string `json:"type"`
			}
			if json.Unmarshal(raw, &tools) != nil || len(tools) > 16 {
				return "", errors.New("invalid tool definitions")
			}
			for _, tool := range tools {
				if tool.Type != "function" {
					return "", errors.New("only client-executed function tools are supported")
				}
			}
		}
	case "response.create":
		if _, ok := event["response"]; ok {
			return "", errors.New("Qwen response.create uses the pinned session settings")
		}
	case "input_audio_buffer.append", "input_image_buffer.append":
		field := "audio"
		if kind == "input_image_buffer.append" {
			field = "image"
		}
		var encoded string
		if json.Unmarshal(event[field], &encoded) != nil || encoded == "" {
			return "", errors.New("media payload must be nonempty Base64")
		}
		if _, err := base64.StdEncoding.DecodeString(encoded); err != nil {
			return "", errors.New("invalid Base64 media")
		}
		if field == "image" && len(encoded) > 256<<10 {
			return "", errors.New("Qwen images must not exceed 256 KiB encoded")
		}
	case "conversation.item.create":
		var item struct {
			Type   string  `json:"type"`
			CallID string  `json:"call_id"`
			Output *string `json:"output"`
		}
		if json.Unmarshal(event["item"], &item) != nil || item.Type != "function_call_output" || item.CallID == "" || item.Output == nil {
			return "", errors.New("only Qwen function_call_output conversation items are supported")
		}
	case "input_audio_buffer.commit", "input_audio_buffer.clear", "response.cancel":
	default:
		return "", errors.New("unsupported Qwen realtime event")
	}
	return kind, nil
}

func validVoice(raw json.RawMessage) bool {
	var voice string
	return json.Unmarshal(raw, &voice) == nil && (voice == "Tina" || voice == "Ethan")
}

func validateAudio(raw json.RawMessage) error {
	var audio map[string]struct {
		Format struct {
			Type       string `json:"type"`
			SampleRate int    `json:"sample_rate"`
		} `json:"format"`
		Voice json.RawMessage `json:"voice"`
	}
	if json.Unmarshal(raw, &audio) != nil || audio == nil {
		return errors.New("invalid Qwen audio configuration")
	}
	for direction, config := range audio {
		if direction != "input" && direction != "output" {
			return errors.New("unsupported audio direction")
		}
		if config.Format.Type != "" && config.Format.Type != "pcm" && config.Format.Type != "wav" {
			return errors.New("unsupported audio encoding")
		}
		switch config.Format.SampleRate {
		case 0, 8000, 16000, 24000, 48000:
		default:
			return errors.New("unsupported audio sample rate")
		}
		if len(config.Voice) > 0 && !validVoice(config.Voice) {
			return errors.New("unsupported Qwen voice")
		}
	}
	return nil
}

// Completion is the actual usage of one uniquely identified Qwen response.
// 版本: v0.0.28
// 日期: 2026-11-23
type Completion struct {
	ID           string
	InputTokens  int64
	OutputTokens int64
	UsageJSON    string
}

// ParseCompletion requires actual usage, retaining all modality-specific metering fields.
// 版本: v0.0.28
// 日期: 2026-11-23
func ParseCompletion(data []byte) (*Completion, error) {
	var event struct {
		Type     string `json:"type"`
		Response struct {
			ID    string          `json:"id"`
			Usage json.RawMessage `json:"usage"`
		} `json:"response"`
	}
	if json.Unmarshal(data, &event) != nil || event.Type != "response.done" || event.Response.ID == "" {
		return nil, errors.New("invalid response completion")
	}
	var usage struct {
		Input  *int64 `json:"input_tokens"`
		Output *int64 `json:"output_tokens"`
	}
	if json.Unmarshal(event.Response.Usage, &usage) != nil || usage.Input == nil || usage.Output == nil || *usage.Input < 0 || *usage.Output < 0 || *usage.Input > MaxInputTokens || *usage.Output > MaxOutputTokens {
		return nil, errors.New("missing or out-of-bounds realtime usage; reconciliation required")
	}
	return &Completion{ID: event.Response.ID, InputTokens: *usage.Input, OutputTokens: *usage.Output, UsageJSON: string(event.Response.Usage)}, nil
}

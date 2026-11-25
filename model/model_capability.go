// model_capability.go 统一模型能力目录与操作约束
// Resolves provider model capabilities without claiming gateway implementation support.
// 版本: v0.0.28
// 日期: 2026-11-23
// 作者: Codex
package model

import (
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"gorm.io/gorm"
)

// OperationLimits binds supported values and maxima to one operation.
// 版本: v0.0.28
// 日期: 2026-11-23
type OperationLimits struct {
	Formats            []string `json:"formats,omitempty"`
	Voices             []string `json:"voices,omitempty"`
	Sizes              []string `json:"sizes,omitempty"`
	Resolutions        []string `json:"resolutions,omitempty"`
	AspectRatios       []string `json:"aspect_ratios,omitempty"`
	Dimensions         []int    `json:"dimensions,omitempty"`
	MaxInputTokens     int      `json:"max_input_tokens,omitempty"`
	MaxOutputTokens    int      `json:"max_output_tokens,omitempty"`
	MaxInputCharacters int      `json:"max_input_characters,omitempty"`
	MaxItems           int      `json:"max_items,omitempty"`
	MinDurationSeconds float64  `json:"min_duration_seconds,omitempty"`
	MaxDurationSeconds float64  `json:"max_duration_seconds,omitempty"`
	MinSpeed           float64  `json:"min_speed,omitempty"`
	MaxSpeed           float64  `json:"max_speed,omitempty"`
}

// OperationCapability describes one operation, its execution modes and constraints.
// 版本: v0.0.28
// 日期: 2026-11-23
type OperationCapability struct {
	Modes            []string        `json:"modes"`
	InputModalities  []string        `json:"input_modalities,omitempty"`
	OutputModalities []string        `json:"output_modalities,omitempty"`
	Limits           OperationLimits `json:"limits,omitempty"`
	Features         []string        `json:"features,omitempty"`
}

// ModelCapabilities is declared model metadata, separate from adapter implementation and health.
// 版本: v0.0.28
// 日期: 2026-11-23
type ModelCapabilities struct {
	PrimaryCategory  string                         `json:"primary_category"`
	Operations       map[string]OperationCapability `json:"operations"`
	Source           string                         `json:"source"`
	InputModalities  []string                       `json:"input_modalities,omitempty"`
	OutputModalities []string                       `json:"output_modalities,omitempty"`
}

var modelCapabilityCategories = map[string]bool{"text": true, "image": true, "video": true, "audio": true, "embedding": true, "rerank": true, "moderation": true, "unknown": true}
var modelCapabilityOperations = map[string]bool{"chat.generate": true, "image.generate": true, "image.edit": true, "image.variation": true, "audio.speech": true, "audio.transcribe": true, "audio.translate": true, "video.generate": true, "video.edit": true, "embedding.create": true, "rerank.score": true, "moderation.check": true}

// ValidateModelCapabilities rejects invalid operation descriptors at the administrative boundary.
// 版本: v0.0.28
// 日期: 2026-11-23
func ValidateModelCapabilities(c ModelCapabilities) error {
	if !modelCapabilityCategories[c.PrimaryCategory] {
		return errors.New("invalid primary_category")
	}
	for operation, capability := range c.Operations {
		if !modelCapabilityOperations[operation] || len(capability.Modes) == 0 {
			return fmt.Errorf("invalid operation or missing modes: %s", operation)
		}
		for _, mode := range capability.Modes {
			if mode != "sync" && mode != "stream" && mode != "async" && mode != "realtime" {
				return fmt.Errorf("invalid execution mode: %s", mode)
			}
		}
		l := capability.Limits
		if l.MaxItems < 0 || l.MaxInputTokens < 0 || l.MaxOutputTokens < 0 || l.MaxInputCharacters < 0 || l.MinDurationSeconds < 0 || l.MaxDurationSeconds < 0 || l.MinSpeed < 0 || l.MaxSpeed < 0 {
			return errors.New("capability limits cannot be negative")
		}
		if l.MaxDurationSeconds > 0 && l.MinDurationSeconds > l.MaxDurationSeconds || l.MaxSpeed > 0 && l.MinSpeed > l.MaxSpeed {
			return errors.New("capability range is inverted")
		}
	}
	return nil
}

// ResolveModelCapabilities uses manual overrides, explicit catalog metadata, builtins, then display-only inference.
// 版本: v0.0.28
// 日期: 2026-11-23
func ResolveModelCapabilities(providerID, modelName string, catalog *ModelCatalog) ModelCapabilities {
	if catalog != nil {
		for _, candidate := range []struct{ value, source string }{{catalog.CapabilityOverride, "override"}, {catalog.Capabilities, "catalog"}} {
			if strings.TrimSpace(candidate.value) == "" {
				continue
			}
			var c ModelCapabilities
			if json.Unmarshal([]byte(candidate.value), &c) == nil && ValidateModelCapabilities(c) == nil {
				c.Source = candidate.source
				return normalizeCapability(c)
			}
			// Invalid stored declarations never silently turn into permissive guessed capabilities.
			return ModelCapabilities{PrimaryCategory: "unknown", Operations: map[string]OperationCapability{}, Source: "invalid_catalog"}
		}
	}
	c := builtinModelCapabilities(providerID, modelName)
	if c.PrimaryCategory != "unknown" {
		return normalizeCapability(c)
	}
	if catalog != nil {
		var input, output []string
		_ = json.Unmarshal([]byte(catalog.InputModalities), &input)
		_ = json.Unmarshal([]byte(catalog.OutputModalities), &output)
		if len(output) > 0 {
			category, operation := "unknown", ""
			for _, out := range output {
				switch out {
				case "video":
					category, operation = "video", "video.generate"
				case "image":
					if category != "video" {
						category, operation = "image", "image.generate"
					}
				case "audio":
					if category == "unknown" {
						category, operation = "audio", "audio.speech"
					}
				case "text":
					if category == "unknown" {
						category, operation = "text", "chat.generate"
					}
				}
			}
			if operation != "" {
				return normalizeCapability(ModelCapabilities{PrimaryCategory: category, Source: "catalog_modalities", InputModalities: input, OutputModalities: output, Operations: map[string]OperationCapability{operation: {Modes: []string{"sync"}, InputModalities: input, OutputModalities: output}}})
			}
		}
	}
	category, operation := inferModelOperation(modelName)
	c = ModelCapabilities{PrimaryCategory: category, Source: "name_inference", Operations: map[string]OperationCapability{}}
	if operation != "" {
		c.Operations[operation] = OperationCapability{Modes: []string{"sync"}}
	}
	return c
}

func normalizeCapability(c ModelCapabilities) ModelCapabilities {
	if c.Operations == nil {
		c.Operations = map[string]OperationCapability{}
	}
	if len(c.InputModalities) == 0 || len(c.OutputModalities) == 0 {
		for _, op := range c.Operations {
			for _, v := range op.InputModalities {
				c.InputModalities = appendUniqueCapability(c.InputModalities, v)
			}
			for _, v := range op.OutputModalities {
				c.OutputModalities = appendUniqueCapability(c.OutputModalities, v)
			}
		}
	}
	return c
}
func appendUniqueCapability(values []string, value string) []string {
	for _, v := range values {
		if v == value {
			return values
		}
	}
	return append(values, value)
}

func builtinModelCapabilities(provider, name string) ModelCapabilities {
	c := ModelCapabilities{PrimaryCategory: "unknown", Source: "builtin", Operations: map[string]OperationCapability{}}
	name = strings.ToLower(name)
	op := func(category, operation string, modes, input, output []string, limits OperationLimits) {
		c.PrimaryCategory = category
		c.Operations[operation] = OperationCapability{Modes: modes, InputModalities: input, OutputModalities: output, Limits: limits}
	}
	if provider == "ali" {
		name = AliOperationModelFamily(name)
		switch {
		case name == "qwen3.5-omni-flash-realtime" || name == "qwen3.5-omni-plus-realtime":
			op("audio", "chat.generate", []string{"realtime"}, []string{"text", "audio", "image"}, []string{"text", "audio"}, OperationLimits{})
		case name == "gte-rerank-v2":
			op("rerank", "rerank.score", []string{"sync"}, []string{"text"}, []string{"score"}, OperationLimits{})
		case name == "qwen3-asr-flash":
			op("audio", "audio.transcribe", []string{"sync"}, []string{"audio"}, []string{"text"}, OperationLimits{})
		case name == "wan2.6-t2v":
			op("video", "video.generate", []string{"async"}, []string{"text"}, []string{"video"}, OperationLimits{Resolutions: []string{"720P", "1080P"}, MinDurationSeconds: 5, MaxDurationSeconds: 15})
		case name == "wan2.7-image" || name == "wan2.7-image-pro":
			op("image", "image.generate", []string{"sync"}, []string{"text"}, []string{"image"}, OperationLimits{MaxItems: 4})
			c.Operations["image.edit"] = OperationCapability{Modes: []string{"sync"}, InputModalities: []string{"text", "image"}, OutputModalities: []string{"image"}, Limits: OperationLimits{MaxItems: 4}}
		case name == "wan2.6-t2i":
			op("image", "image.generate", []string{"sync"}, []string{"text"}, []string{"image"}, OperationLimits{MaxItems: 4})
		case strings.HasPrefix(name, "cosyvoice-v3"):
			// cosyvoice-v3 系列的音色名带 _v3 后缀（2026-11-25 线上实测：
			// longanhuan_v3 / longwan_v3 / longfei_v3 可用，longyu_v3 上游报 418）。
			op("audio", "audio.speech", []string{"sync"}, []string{"text"}, []string{"audio"}, OperationLimits{Formats: []string{"wav", "mp3", "pcm", "opus"}, Voices: []string{"longanhuan_v3", "longwan_v3", "longfei_v3"}, MaxInputCharacters: 4096, MinSpeed: .5, MaxSpeed: 2})
		case strings.HasPrefix(name, "qwen-audio-3.") && strings.Contains(name, "tts"):
			// qwen-audio-3.1-tts 系列的音色名带 _v3.1 后缀（2026-11-25 线上实测：
			// longanhuan_v3.1 / longwan_v3.1 / longfei_v3.1 可用）。
			// 误用 cosyvoice 的音色名会打到上游变成 "[cosyvoice:]Engine error [411]"，
			// 声明白名单后网关本地就能拦下并直接告诉调用方可用音色。
			op("audio", "audio.speech", []string{"sync"}, []string{"text"}, []string{"audio"}, OperationLimits{Formats: []string{"wav", "mp3", "pcm", "opus"}, Voices: []string{"longanhuan_v3.1", "longwan_v3.1", "longfei_v3.1"}, MaxInputCharacters: 4096, MinSpeed: .5, MaxSpeed: 2})
		case strings.HasPrefix(name, "cosyvoice-"):
			op("audio", "audio.speech", []string{"sync"}, []string{"text"}, []string{"audio"}, OperationLimits{Formats: []string{"wav", "mp3", "pcm", "opus"}, MaxInputCharacters: 4096, MinSpeed: .5, MaxSpeed: 2})
		case strings.HasPrefix(name, "qwen3-vl-embedding"):
			op("embedding", "embedding.create", []string{"sync"}, []string{"text", "image", "video"}, []string{"vector"}, OperationLimits{})
		}
	}
	if provider == "doubao" && strings.HasPrefix(name, "seed-tts") {
		// 火山语音合成 2.0（seed-tts-2.0）由 doubao 适配器的 V3 原生协议实现。
		op("audio", "audio.speech", []string{"sync"}, []string{"text"}, []string{"audio"}, OperationLimits{Formats: []string{"wav", "pcm", "ogg_opus", "mp3"}, MaxInputCharacters: 1024, MinSpeed: 0.5, MaxSpeed: 2})
	}
	if len(c.Operations) > 0 {
		return c
	}
	if provider == "openai" || provider == "openai_compatible" || provider == "azure" {
		switch {
		case name == "tts-1" || name == "tts-1-hd" || name == "gpt-4o-mini-tts":
			op("audio", "audio.speech", []string{"sync"}, []string{"text"}, []string{"audio"}, OperationLimits{})
		case name == "whisper-1":
			op("audio", "audio.transcribe", []string{"sync"}, []string{"audio"}, []string{"text"}, OperationLimits{})
			c.Operations["audio.translate"] = c.Operations["audio.transcribe"]
		case strings.HasPrefix(name, "text-embedding-"):
			op("embedding", "embedding.create", []string{"sync"}, []string{"text"}, []string{"vector"}, OperationLimits{})
		case name == "dall-e-2" || name == "dall-e-3" || strings.HasPrefix(name, "gpt-image-"):
			op("image", "image.generate", []string{"sync"}, []string{"text"}, []string{"image"}, OperationLimits{})
		}
	}
	if len(c.Operations) > 0 {
		return c
	}
	knownChatProvider := map[string]bool{"openai": true, "openai_compatible": true, "azure": true, "openrouter": true, "anthropic": true, "ali": true, "alibailian": true, "gemini": true, "gemini_openai_compatible": true, "deepseek": true, "doubao": true, "zhipu": true}
	category, operation := inferModelOperation(name)
	if knownChatProvider[provider] && category == "text" && operation == "chat.generate" {
		input := []string{"text"}
		if strings.Contains(name, "-vl") || strings.Contains(name, "vision") || strings.HasPrefix(name, "gpt-4o") || strings.HasPrefix(name, "gemini-") || strings.HasPrefix(name, "claude-") {
			input = append(input, "image")
		}
		op("text", "chat.generate", []string{"sync", "stream"}, input, []string{"text"}, OperationLimits{})
	}
	return c
}

func inferModelOperation(name string) (string, string) {
	name = strings.ToLower(name)
	for _, rule := range []struct {
		category, operation string
		markers             []string
	}{
		{"rerank", "rerank.score", []string{"rerank"}},
		{"embedding", "embedding.create", []string{"embed", "bge-", "gte-", "e5-", "voyage"}},
		{"moderation", "moderation.check", []string{"moderation", "guard", "safety"}},
		{"video", "video.generate", []string{"video", "t2v", "i2v", "seedance", "sora", "kling", "veo"}},
		{"image", "image.generate", []string{"image", "dall-e", "flux", "sdxl", "stable-diffusion", "cogview", "wanx", "seedream", "kolors", "recraft"}},
		{"audio", "audio.speech", []string{"tts", "cosyvoice", "speech", "voice"}},
		{"audio", "audio.transcribe", []string{"whisper", "asr", "transcri"}},
		{"audio", "chat.generate", []string{"audio", "omni"}},
		{"text", "chat.generate", []string{"gpt-", "claude", "qwen", "deepseek", "gemini", "glm-", "llama", "mistral", "doubao", "kimi", "command-r"}},
	} {
		for _, marker := range rule.markers {
			if strings.Contains(name, marker) {
				return rule.category, rule.operation
			}
		}
	}
	return "unknown", ""
}

// GetModelCapabilities loads optional provider metadata and resolves the declared model capability.
// 版本: v0.0.28
// 日期: 2026-11-23
func GetModelCapabilities(providerID, modelName string) (ModelCapabilities, error) {
	db := DB
	cache := modelCapabilityCache(db)
	cache.mu.Lock()
	revision := cache.revision.Load()
	key := providerID + "\x00" + modelName
	entry, ok := cache.entries[key]
	cache.mu.Unlock()
	if ok && entry.revision == revision && time.Now().Before(entry.expires) {
		return cloneModelCapabilities(entry.value), nil
	}
	var row ModelCatalog
	err := db.Where("provider = ? AND model_name = ?", providerID, modelName).Take(&row).Error
	if err != nil && !errors.Is(err, gorm.ErrRecordNotFound) {
		return ModelCapabilities{}, err
	}
	var metadata *ModelCatalog
	if err == nil {
		metadata = &row
	}
	value := ResolveModelCapabilities(providerID, modelName, metadata)
	cache.mu.Lock()
	defer cache.mu.Unlock()
	// Never publish a stale database read over a configuration invalidation.
	if cache.revision.Load() != revision {
		return cloneModelCapabilities(value), nil
	}
	if len(cache.entries) >= 4096 {
		cache.entries = map[string]capabilityCacheEntry{}
	}
	cache.entries[key] = capabilityCacheEntry{value: value, revision: revision, expires: time.Now().Add(30 * time.Second)}
	return cloneModelCapabilities(value), nil
}

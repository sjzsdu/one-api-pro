// Package modelcatalog holds the curated set of "currently mainstream" models
// used to prefill the channel configuration form.
//
// WHY THIS EXISTS
// The adaptor ModelList constants are a *capability* list: they must keep old
// model names forever so existing channels keep routing. That makes them a
// poor source for "what should a new channel use today" — e.g. qwen-vl-plus is
// still in the ali list long after Aliyun renamed it to qwen3-vl-plus.
//
// This catalog is the *recommendation* list. It is deliberately small, dated,
// and time-aware:
//   - AsOf is the last human calibration date of the whole catalog.
//   - Each FeaturedModel may carry Deprecated (YYYY-MM-DD). Once today reaches
//     that date the model is no longer returned by the API — expiry is applied
//     by code, so a stale entry cannot silently keep being recommended.
//   - Replacement points maintainers/users at the successor model.
//
// MAINTENANCE: when a provider launches/retires a mainstream model, update the
// entries below and bump AsOf. Retirements only need a Deprecated date — no
// code change is required for the filter to take effect.
package modelcatalog

import (
	"sort"
	"time"

	"github.com/modelbus/one-api-pro/relay/channeltype"
)

const dateLayout = "2006-01-02"

// AsOf is the date this catalog was last calibrated against provider docs.
// The API returns it to the UI so operators can see how fresh the defaults are.
const AsOf = "2026-11-19"

// FeaturedModel is one recommended model.
type FeaturedModel struct {
	ID          string `json:"id"`
	Name        string `json:"name,omitempty"`
	Group       string `json:"group,omitempty"`       // chat | reasoning | vision | embedding | code
	Replacement string `json:"replacement,omitempty"` // successor shown when this model is retired
	Deprecated  string `json:"-"`                     // YYYY-MM-DD; internal, expired entries are filtered out
}

// Preset is the recommended configuration for one channel type.
type Preset struct {
	BaseURL string          `json:"base_url"`
	Name    string          `json:"name"`
	Models  []FeaturedModel `json:"models"`
}

// catalog is indexed by legacy channel type. Only mainstream providers are
// listed here. Any type not present falls back to the adaptor's full capability
// list (see controller.ListChannelDefaults).
var catalog = map[int]Preset{
	channeltype.OpenAI: {
		BaseURL: "https://api.openai.com",
		Name:    "OpenAI",
		Models: []FeaturedModel{
			{ID: "gpt-5.1", Name: "GPT-5.1", Group: "chat"},
			{ID: "gpt-5.1-mini", Name: "GPT-5.1 Mini", Group: "chat"},
			{ID: "gpt-5.1-nano", Name: "GPT-5.1 Nano", Group: "chat"},
			{ID: "gpt-5", Name: "GPT-5", Group: "reasoning"},
			{ID: "gpt-5-mini", Name: "GPT-5 Mini", Group: "reasoning"},
		},
	},
	channeltype.DeepSeek: {
		BaseURL: "https://api.deepseek.com",
		Name:    "DeepSeek",
		Models: []FeaturedModel{
			{ID: "deepseek-v4-flash", Name: "DeepSeek V4 Flash", Group: "chat"},
			{ID: "deepseek-v4-pro", Name: "DeepSeek V4 Pro", Group: "reasoning"},
			{ID: "deepseek-chat", Name: "DeepSeek Chat", Group: "chat"},
			{ID: "deepseek-reasoner", Name: "DeepSeek Reasoner", Group: "reasoning"},
		},
	},
	// Aliyun Bailian OpenAI-compatible mode. BaseURL intentionally includes
	// /compatible-mode: the deployed adaptor appends /v1/chat/completions, so a
	// bare dashscope.aliyuncs.com produced 404 (verified 2026-11-19).
	channeltype.AliBailian: {
		BaseURL: "https://dashscope.aliyuncs.com/compatible-mode",
		Name:    "阿里云百炼",
		Models: []FeaturedModel{
			{ID: "qwen-plus", Name: "通义千问 Plus", Group: "chat"},
			{ID: "qwen3-max", Name: "通义千问 Max", Group: "reasoning"},
			{ID: "qwen-flash", Name: "通义千问 Flash", Group: "chat"},
			{ID: "qwen3-vl-plus", Name: "通义千问 VL Plus", Group: "vision"},
			// qwen-vl-plus is a retired name; expired so it never gets prefilled.
			{ID: "qwen-vl-plus", Name: "通义千问 VL Plus（旧）", Group: "vision", Deprecated: "2026-06-01", Replacement: "qwen3-vl-plus"},
		},
	},
	channeltype.Ali: {
		BaseURL: "https://dashscope.aliyuncs.com",
		Name:    "阿里云灵积",
		Models: []FeaturedModel{
			{ID: "qwen3.7-max", Name: "通义千问 3.7 Max", Group: "reasoning"},
			{ID: "qwen3.7-plus", Name: "通义千问 3.7 Plus", Group: "chat"},
			{ID: "qwen3.6-flash", Name: "通义千问 3.6 Flash", Group: "chat"},
			{ID: "qwen3-vl-plus", Name: "通义千问 VL Plus", Group: "vision"},
			{ID: "qwen-vl-plus", Name: "通义千问 VL Plus（旧）", Group: "vision", Deprecated: "2026-06-01", Replacement: "qwen3-vl-plus"},
		},
	},
	channeltype.Gemini: {
		BaseURL: "https://generativelanguage.googleapis.com",
		Name:    "Google Gemini",
		Models: []FeaturedModel{
			{ID: "gemini-2.5-pro", Name: "Gemini 2.5 Pro", Group: "reasoning"},
			{ID: "gemini-2.5-flash", Name: "Gemini 2.5 Flash", Group: "chat"},
			{ID: "gemini-2.5-flash-lite", Name: "Gemini 2.5 Flash Lite", Group: "chat"},
		},
	},
	channeltype.GeminiOpenAICompatible: {
		BaseURL: "https://generativelanguage.googleapis.com/v1beta/openai/",
		Name:    "Gemini OpenAI 兼容",
		Models: []FeaturedModel{
			{ID: "gemini-2.5-pro", Name: "Gemini 2.5 Pro", Group: "reasoning"},
			{ID: "gemini-2.5-flash", Name: "Gemini 2.5 Flash", Group: "chat"},
			{ID: "gemini-2.5-flash-lite", Name: "Gemini 2.5 Flash Lite", Group: "chat"},
		},
	},
	channeltype.Zhipu: {
		BaseURL: "https://open.bigmodel.cn",
		Name:    "智谱 GLM",
		Models: []FeaturedModel{
			{ID: "glm-5.2", Name: "GLM 5.2", Group: "reasoning"},
			{ID: "glm-5.1", Name: "GLM 5.1", Group: "chat"},
			{ID: "glm-5", Name: "GLM 5", Group: "chat"},
		},
	},
	channeltype.Doubao: {
		BaseURL: "https://ark.cn-beijing.volces.com",
		Name:    "火山方舟豆包",
		Models: []FeaturedModel{
			{ID: "doubao-seed-1-6", Name: "豆包 Seed 1.6", Group: "chat"},
			{ID: "doubao-seed-1-6-thinking", Name: "豆包 Seed 1.6 Thinking", Group: "reasoning"},
			{ID: "doubao-seed-1-6-flash", Name: "豆包 Seed 1.6 Flash", Group: "chat"},
			{ID: "doubao-seed-1-6-vision", Name: "豆包 Seed 1.6 Vision", Group: "vision"},
		},
	},
}

// expired reports whether a YYYY-MM-DD date has been reached by now.
func expired(date string, now time.Time) bool {
	if date == "" {
		return false
	}
	d, err := time.ParseInLocation(dateLayout, date, time.Local)
	if err != nil {
		return false // malformed dates never hide a model; surface it to maintainers
	}
	today, err := time.ParseInLocation(dateLayout, now.Format(dateLayout), time.Local)
	if err != nil {
		return false
	}
	return !today.Before(d)
}

// Get returns the preset for a legacy channel type with expired models removed.
func Get(legacyType int) (Preset, bool) {
	return GetAt(legacyType, time.Now())
}

// GetAt is Get with an injectable clock (used by tests).
func GetAt(legacyType int, now time.Time) (Preset, bool) {
	p, ok := catalog[legacyType]
	if !ok {
		return Preset{}, false
	}
	models := make([]FeaturedModel, 0, len(p.Models))
	for _, m := range p.Models {
		if expired(m.Deprecated, now) {
			continue
		}
		models = append(models, m)
	}
	p.Models = models
	return p, true
}

// All returns every preset keyed by legacy channel type, filtered by today.
func All() map[int]Preset {
	return AllAt(time.Now())
}

// AllAt is All with an injectable clock (used by tests).
func AllAt(now time.Time) map[int]Preset {
	out := make(map[int]Preset, len(catalog))
	for t := range catalog {
		if p, ok := GetAt(t, now); ok {
			out[t] = p
		}
	}
	return out
}

// Types returns the sorted list of legacy channel types that have a curated preset.
func Types() []int {
	ids := make([]int, 0, len(catalog))
	for t := range catalog {
		ids = append(ids, t)
	}
	sort.Ints(ids)
	return ids
}

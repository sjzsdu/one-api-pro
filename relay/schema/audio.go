// audio.go 网关语音请求与归一化响应
// Defines speech requests and buffered audio results with billing usage.
// 版本: v0.0.28
// 日期: 2026-11-23
// 作者: Codex
package model

// AudioSpeechRequest 是 OpenAI speech 协议的标准请求结构，网关内部统一使用。
//
// 历史上该结构只存在于 relay/adaptor/openai 包（TextToSpeechRequest），导致
// provider 适配器拿不到强类型请求、无法做协议转换。把它上移到 relay/schema
// 后，任何渠道都可通过实现 adaptor.AudioSpeechRequester 接入原生 TTS 协议。
//
// Speed 使用指针以区分「未传」与「显式 0」——透传时应保持字段缺失，避免
// 上游把 0 解释成合法的静止语速。
// 版本: v0.0.28
// 日期: 2026-11-23
type AudioSpeechRequest struct {
	Model          string   `json:"model"`
	Input          string   `json:"input"`
	Voice          string   `json:"voice,omitempty"`
	Speed          *float64 `json:"speed,omitempty"`
	ResponseFormat string   `json:"response_format,omitempty"`
}

// AudioSpeechResponse carries validated audio and explicit provider billing units.
// 版本: v0.0.28
// 日期: 2026-11-23
type AudioSpeechResponse struct {
	Body        []byte
	ContentType string
	Characters  *int
	TokenUsage  *Usage
}

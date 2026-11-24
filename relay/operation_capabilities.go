// operation_capabilities.go 网关实际已实现操作的能力上限
// Reports executable operation modes independently of catalog declarations.
// 版本: v0.0.28
// 日期: 2026-11-23
// 作者: Codex
package relay

import (
	"github.com/modelbus/one-api-pro/model"
	"strings"
)

// RuntimeOperationModes describes implementation support, never account access or upstream health.
// 版本: v0.0.28
// 日期: 2026-11-23
func RuntimeOperationModes(providerID, modelName string) map[string][]string {
	result := map[string][]string{}
	name := strings.ToLower(modelName)
	if GetAdaptorByChannelID(providerID) == nil {
		return result
	}
	if providerID == "ali" {
		name = model.AliOperationModelFamily(name)
		switch {
		case name == "qwen3.5-omni-flash-realtime" || name == "qwen3.5-omni-plus-realtime":
			result["chat.generate"] = []string{"realtime"}
		case name == "gte-rerank-v2":
			result["rerank.score"] = []string{"sync"}
		case name == "qwen3-asr-flash":
			result["audio.transcribe"] = []string{"sync"}
		case name == "wan2.6-t2v":
			result["video.generate"] = []string{"async"}
		case name == "wan2.7-image" || name == "wan2.7-image-pro":
			result["image.generate"] = []string{"sync"}
			result["image.edit"] = []string{"sync"}
		case name == "wan2.6-t2i":
			// 旧万相文生图上游走 text2image/image-synthesis 异步协议并由网关轮询，
			// 但对客户端仍是 /v1/images/generations 同步返回，故对外模式为 sync。
			result["image.generate"] = []string{"sync"}
		case strings.HasPrefix(name, "cosyvoice-") || strings.Contains(name, "tts"):
			result["audio.speech"] = []string{"sync"}
		case strings.HasPrefix(name, "qwen3-vl-embedding"):
			result["embedding.create"] = []string{"sync"}
		case strings.HasPrefix(name, "wanx"):
			result["image.generate"] = []string{"sync"}
		}
	}
	if len(result) > 0 {
		return result
	}
	// Runtime support is an adapter protocol ceiling, not a name classifier.
	// An administrator can declare a custom upstream name within this ceiling.
	result["chat.generate"] = []string{"sync", "stream"}
	if providerID == "openai" || providerID == "openai_compatible" || providerID == "azure" {
		for _, operation := range []string{"embedding.create", "image.generate", "audio.speech", "audio.transcribe", "audio.translate", "moderation.check"} {
			result[operation] = []string{"sync"}
		}
	} else if providerID == "ali" {
		result["embedding.create"] = []string{"sync"}
		// Legacy ASR/translation bindings use the existing compatible multipart
		// relay. This ceiling does not claim a native typed protocol for them.
		result["audio.transcribe"] = []string{"sync"}
		result["audio.translate"] = []string{"sync"}
	} else if providerID == "zhipu" || providerID == "replicate" || providerID == "baidu" {
		result["image.generate"] = []string{"sync"}
	} else if providerID == "doubao" && (name == "seed-tts-2.0" || strings.HasPrefix(name, "seed-tts")) {
		// 火山语音合成 2.0 由 doubao 适配器的 V3 原生协议实现。
		result["audio.speech"] = []string{"sync"}
	}
	return result
}

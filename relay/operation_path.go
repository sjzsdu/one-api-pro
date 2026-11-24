// operation_path.go 推理请求路径到能力操作映射
// Maps public inference routes to operation and transport mode.
// 版本: v0.0.28
// 日期: 2026-11-23
// 作者: Codex
package relay

import "strings"

// OperationForPath returns the requested operation independently of the selected model.
// 版本: v0.0.28
// 日期: 2026-11-23
func OperationForPath(path string) (string, string) {
	switch {
	case path == "/v1/realtime":
		return "chat.generate", "realtime"
	case path == "/v1/media/jobs":
		return "video.generate", "async"
	case strings.HasPrefix(path, "/v1/images/generations"):
		return "image.generate", "sync"
	case strings.HasPrefix(path, "/v1/images/edits"):
		return "image.edit", "sync"
	case strings.HasPrefix(path, "/v1/images/variations"):
		return "image.variation", "sync"
	case strings.HasPrefix(path, "/v1/audio/speech"):
		return "audio.speech", "sync"
	case strings.HasPrefix(path, "/v1/audio/transcriptions"):
		return "audio.transcribe", "sync"
	case strings.HasPrefix(path, "/v1/audio/translations"):
		return "audio.translate", "sync"
	case strings.Contains(path, "embeddings"):
		return "embedding.create", "sync"
	case path == "/v1/rerank":
		return "rerank.score", "sync"
	case path == "/v1/moderations":
		return "moderation.check", "sync"
	default:
		return "chat.generate", "sync"
	}
}

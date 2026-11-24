// operations.go 可选强类型媒体操作接口
// Adds opt-in operation interfaces without changing the legacy adapter contract.
// 版本: v0.0.28
// 日期: 2026-11-23
// 作者: Codex
package adaptor

import (
	"context"
	"github.com/modelbus/one-api-pro/relay/meta"
	schema "github.com/modelbus/one-api-pro/relay/schema"
)

// Reranker supports a native rerank protocol.
// 版本: v0.0.28
// 日期: 2026-11-23
type Reranker interface {
	Rerank(context.Context, *meta.Meta, *schema.RerankRequest) (*schema.OperationResult, error)
}

// AudioSpeechTokenMeter declares whether speech needs estimated token-based reservation.
// 版本: v0.0.28
// 日期: 2026-11-23
type AudioSpeechTokenMeter interface{ SpeechUsesTokens(modelName string) bool }

// Transcriber supports synchronous audio transcription.
// 版本: v0.0.28
// 日期: 2026-11-23
type Transcriber interface {
	Transcribe(context.Context, *meta.Meta, *schema.TranscriptionRequest) (*schema.OperationResult, error)
}

// ImageEditor supports reference-image editing.
// 版本: v0.0.28
// 日期: 2026-11-23
type ImageEditor interface {
	EditImage(context.Context, *meta.Meta, *schema.ImageEditRequest) (*schema.OperationResult, error)
}

// VideoJobProvider submits, observes and requests cancellation of native video tasks.
// 版本: v0.0.28
// 日期: 2026-11-23
type VideoJobProvider interface {
	SubmitVideo(context.Context, *meta.Meta, *schema.VideoRequest) (*schema.VideoTask, error)
	PollVideo(context.Context, *meta.Meta, string) (*schema.VideoTask, error)
	CancelVideo(context.Context, *meta.Meta, string) error
}

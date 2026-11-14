//go:build !cgo

// embedder_onnx_stub.go 无 CGO 构建时的 ONNX embedder 纯 Go 占位实现
// Pure-Go stub for the optional ONNX embedder when CGO is disabled.
// 版本: v0.0.28
// 日期: 2026-11-14
// 作者: opencode

package modelrouter

import (
	"context"
	"errors"
)

// ErrONNXUnavailable 说明当前二进制未编入 ONNX Runtime 支持。
// ErrONNXUnavailable reports that this binary was built without ONNX Runtime.
var ErrONNXUnavailable = errors.New("ONNX embedder requires a cgo-enabled build (CGO_ENABLED=1); set EMBEDDING_PROVIDER=api or use a cgo-enabled binary for cluster routing")

// Tokenizer supplies model-specific token IDs.
type Tokenizer interface {
	Encode(text string, maxTokens int) ([]int64, error)
}

// TokenizerFunc adapts a plain function to the Tokenizer interface.
type TokenizerFunc func(text string, maxTokens int) ([]int64, error)

// Encode implements Tokenizer.
func (f TokenizerFunc) Encode(text string, maxTokens int) ([]int64, error) {
	return f(text, maxTokens)
}

// HuggingFaceTokenizer is unavailable in CGO-disabled builds.
type HuggingFaceTokenizer struct{}

// NewHuggingFaceTokenizer always returns ErrONNXUnavailable without cgo.
func NewHuggingFaceTokenizer(path string) (*HuggingFaceTokenizer, error) {
	return nil, ErrONNXUnavailable
}

// Encode implements Tokenizer.
func (t *HuggingFaceTokenizer) Encode(text string, maxTokens int) ([]int64, error) {
	return nil, ErrONNXUnavailable
}

// WordPieceTokenizer is unavailable in CGO-disabled builds.
type WordPieceTokenizer struct{}

// NewWordPieceTokenizer always returns ErrONNXUnavailable without cgo.
func NewWordPieceTokenizer(path string) (*WordPieceTokenizer, error) {
	return nil, ErrONNXUnavailable
}

// Encode implements Tokenizer.
func (t *WordPieceTokenizer) Encode(text string, maxTokens int) ([]int64, error) {
	return nil, ErrONNXUnavailable
}

// ONNXEmbedderConfig mirrors the cgo implementation so callers compile unchanged.
type ONNXEmbedderConfig struct {
	ModelPath      string
	RuntimeLibrary string
	Model          string
	Dimension      int
	MaxTokens      int
	InputNames     []string
	OutputName     string
	Tokenizer      Tokenizer
}

// ONNXEmbedder is a placeholder satisfying the Embedder interface.
type ONNXEmbedder struct{}

// NewONNXEmbedder always returns ErrONNXUnavailable without cgo.
func NewONNXEmbedder(cfg ONNXEmbedderConfig) (*ONNXEmbedder, error) {
	return nil, ErrONNXUnavailable
}

// Dimension implements Embedder.
func (e *ONNXEmbedder) Dimension() int { return 0 }

// Close implements CloseableEmbedder.
func (e *ONNXEmbedder) Close() error { return nil }

// Embed implements Embedder.
func (e *ONNXEmbedder) Embed(ctx context.Context, text string) ([]float64, error) {
	return nil, ErrONNXUnavailable
}

package modelrouter

import (
	"context"
	"strings"

	"github.com/modelbus/one-api-pro/common/config"
	"github.com/modelbus/one-api-pro/common/logger"
)

var DefaultRouter ModelRouter

func InitRouter() {
	strategy := config.ModelRouterStrategy
	if strategy == "" {
		strategy = "random"
	}
	var err error
	DefaultRouter, err = Get(strategy)
	if err != nil {
		logger.SysLog("model router: unknown strategy '" + strategy + "', falling back to random")
		DefaultRouter, _ = Get("random")
	}
	logger.SysLog("model router initialized with strategy: " + DefaultRouter.Name())
}

// PrewarmDefaultRouter initializes the local ONNX embedding runtime before the
// HTTP server starts accepting requests. This also downloads the configured
// embedding model when it is missing from the local cache.
//
// It is deliberately a no-op for non-embedding strategies and API embedders:
// those do not own a local ONNX model to preload.
func PrewarmDefaultRouter(ctx context.Context) (bool, error) {
	router, ok := DefaultRouter.(*EmbeddingModelRouter)
	if !ok || !strings.EqualFold(envOrDefault("EMBEDDING_PROVIDER", "onnx"), "onnx") {
		return false, nil
	}
	return true, router.Prewarm(ctx)
}

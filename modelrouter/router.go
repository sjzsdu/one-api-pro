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

// PrewarmDefaultRouter initializes a router that owns local resources before
// the HTTP server starts accepting requests. Cluster routing uses this to
// download and load its configured embedding model.
func PrewarmDefaultRouter(ctx context.Context) (bool, error) {
	router, ok := DefaultRouter.(interface{ Prewarm(context.Context) error })
	if !ok || !strings.EqualFold(envOrDefault("EMBEDDING_PROVIDER", "onnx"), "onnx") {
		return false, nil
	}
	return true, router.Prewarm(ctx)
}

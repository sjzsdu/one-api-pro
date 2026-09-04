package modelrouter

import (
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

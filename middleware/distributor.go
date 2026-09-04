package middleware

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"

	"github.com/gin-gonic/gin"

	"github.com/modelbus/one-api-pro/channelrouter"
	"github.com/modelbus/one-api-pro/common"
	"github.com/modelbus/one-api-pro/common/config"
	"github.com/modelbus/one-api-pro/common/ctxkey"
	"github.com/modelbus/one-api-pro/common/logger"
	"github.com/modelbus/one-api-pro/model"
	"github.com/modelbus/one-api-pro/modelrouter"
	"github.com/modelbus/one-api-pro/relay/registry"
)

type ModelRequest struct {
	Model string `json:"model" form:"model"`
}

// ModelAutoRoute resolves the virtual "auto" model before quota checks. The
// same resolution is also performed defensively at the start of Distribute so
// callers that compose the middleware directly retain the expected behavior.
func ModelAutoRoute() func(c *gin.Context) {
	return func(c *gin.Context) {
		if err := resolveAutoModel(c); err != nil {
			abortWithMessage(c, http.StatusServiceUnavailable, err.Error())
			return
		}
		c.Next()
	}
}

func Distribute() func(c *gin.Context) {
	return func(c *gin.Context) {
		if err := resolveAutoModel(c); err != nil {
			abortWithMessage(c, http.StatusServiceUnavailable, err.Error())
			return
		}
		ctx := c.Request.Context()
		userId := c.GetInt(ctxkey.Id)
		userGroup, _ := model.CacheGetUserGroup(userId)
		c.Set(ctxkey.Group, userGroup)
		var requestModel string
		var channel *model.Channel
		channelId, ok := c.Get(ctxkey.SpecificChannelId)
		if ok {
			id, err := strconv.Atoi(channelId.(string))
			if err != nil {
				abortWithMessage(c, http.StatusBadRequest, "无效的渠道 Id")
				return
			}
			channel, err = model.GetChannelById(id, true)
			if err != nil {
				abortWithMessage(c, http.StatusBadRequest, "无效的渠道 Id")
				return
			}
			if channel.Status != model.ChannelStatusEnabled {
				abortWithMessage(c, http.StatusForbidden, "该渠道已被禁用")
				return
			}
			if !channel.ContainsGroup(userGroup) {
				abortWithMessage(c, http.StatusForbidden, "当前分组无权使用该渠道")
				return
			}
			requestModel = c.GetString(ctxkey.RequestModel)
			if requestModel != "" && !channel.ContainsModel(requestModel) {
				abortWithMessage(c, http.StatusForbidden, "该渠道不支持请求的模型")
				return
			}
		} else {
			requestModel = c.GetString(ctxkey.RequestModel)
			var err error
			channel, err = selectChannelViaRouter(c, userGroup, requestModel, userId, false)
			if err != nil {
				message := fmt.Sprintf("当前分组 %s 下对于模型 %s 无可用渠道", userGroup, requestModel)
				if channel != nil {
					logger.SysError(fmt.Sprintf("渠道不存在：%d", channel.Id))
					message = "数据库一致性已被破坏，请联系管理员"
				}
				abortWithMessage(c, http.StatusServiceUnavailable, message)
				return
			}
		}
		logger.Debugf(ctx, "user id %d, user group: %s, request model: %s, using channel #%d", userId, userGroup, requestModel, channel.Id)

		sessionKey := ""
		if config.ChannelStickySessionEnabled && userId > 0 && requestModel != "" {
			sessionKey = channelrouter.MakeSessionKey(userId, requestModel)
			c.Set(ctxkey.SessionKey, sessionKey)
		}

		if config.ChannelConcurrencyEnabled && channel.GetMaxConcurrency() > 0 {
			if !channelrouter.DefaultRouter.TryAcquireConcurrency(channel.Id, channel.GetMaxConcurrency()) {
				logger.Debugf(ctx, "channel #%d at concurrency limit (%d), trying alternate", channel.Id, channel.GetMaxConcurrency())
				altChannel, altErr := selectChannelViaRouter(c, userGroup, requestModel, userId, false)
				if altErr == nil && altChannel.Id != channel.Id {
					channel = altChannel
				}
			}
			c.Set(ctxkey.ConcurrencyAcquired, true)
		}

		SetupContextForSelectedChannel(c, channel, requestModel)

		if channelrouter.DefaultRouter != nil && channel.GetRPM() > 0 {
			channelrouter.DefaultRouter.IncrementRPM(channel.Id)
		}

		if sessionKey != "" {
			channelrouter.DefaultRouter.SetStickySession(sessionKey, channel.Id)
		}

		c.Next()

		if val, exists := c.Get(ctxkey.ConcurrencyAcquired); exists && val.(bool) {
			channelrouter.DefaultRouter.ReleaseConcurrency(channel.Id)
		}
	}
}

func resolveAutoModel(c *gin.Context) error {
	if !config.ModelAutoEnabled || c.GetString(ctxkey.RequestModel) != "auto" {
		return nil
	}

	ctx := c.Request.Context()
	userId := c.GetInt(ctxkey.Id)
	group := c.GetString(ctxkey.Group)
	if group == "" {
		var err error
		group, err = model.CacheGetUserGroup(userId)
		if err != nil {
			return fmt.Errorf("failed to get user group for automatic model routing: %w", err)
		}
		c.Set(ctxkey.Group, group)
	}

	request := &modelrouter.ModelSelectRequest{}
	if err := common.UnmarshalBodyReusable(c, request); err != nil {
		return fmt.Errorf("failed to parse request for automatic model routing: %w", err)
	}

	models, err := model.CacheGetGroupModels(ctx, group)
	if err != nil {
		return fmt.Errorf("failed to list models for automatic routing: %w", err)
	}
	request.AvailableModels = filterAvailableModels(models, c.GetString(ctxkey.AvailableModels))
	if modelrouter.DefaultRouter == nil {
		return fmt.Errorf("automatic model router is not initialized")
	}
	selected, err := modelrouter.DefaultRouter.SelectModel(ctx, group, userId, request)
	if err != nil {
		return fmt.Errorf("automatic model routing failed: %w", err)
	}
	if err := rewriteRequestModel(c, selected); err != nil {
		return fmt.Errorf("failed to apply automatically selected model: %w", err)
	}

	c.Set(ctxkey.OriginalRequestModel, request.Model)
	c.Set(ctxkey.RequestModel, selected)
	logger.Debugf(ctx, "user id %d, user group: %s, model router: %s, selected model: %s", userId, group, modelrouter.DefaultRouter.Name(), selected)
	return nil
}

func filterAvailableModels(groupModels []string, tokenModels string) []string {
	allowed := make(map[string]struct{})
	if tokenModels != "" {
		for _, modelName := range strings.Split(tokenModels, ",") {
			modelName = strings.TrimSpace(modelName)
			if modelName != "" && modelName != "auto" {
				allowed[modelName] = struct{}{}
			}
		}
	}

	result := make([]string, 0, len(groupModels))
	seen := make(map[string]struct{}, len(groupModels))
	for _, modelName := range groupModels {
		modelName = strings.TrimSpace(modelName)
		if modelName == "" || modelName == "auto" {
			continue
		}
		if _, ok := seen[modelName]; ok {
			continue
		}
		if tokenModels != "" {
			if _, ok := allowed[modelName]; !ok {
				continue
			}
		}
		seen[modelName] = struct{}{}
		result = append(result, modelName)
	}
	return result
}

func rewriteRequestModel(c *gin.Context, selected string) error {
	body, err := common.GetRequestBody(c)
	if err != nil {
		return err
	}
	var payload map[string]any
	if err := json.Unmarshal(body, &payload); err != nil {
		return err
	}
	payload["model"] = selected
	updated, err := json.Marshal(payload)
	if err != nil {
		return err
	}
	c.Set(ctxkey.KeyRequestBody, updated)
	c.Request.Body = io.NopCloser(bytes.NewReader(updated))
	c.Request.ContentLength = int64(len(updated))
	return nil
}

func selectChannelViaRouter(c *gin.Context, group, modelName string, userId int, ignoreFirstPriority bool) (*model.Channel, error) {
	if channelrouter.DefaultRouter == nil {
		return model.CacheGetRandomSatisfiedChannel(group, modelName, ignoreFirstPriority)
	}

	sessionKey := ""
	if config.ChannelStickySessionEnabled && userId > 0 && modelName != "" {
		sessionKey = channelrouter.MakeSessionKey(userId, modelName)
	}

	candidates := model.GetChannelCandidates(group, modelName)
	if len(candidates) == 0 {
		return model.CacheGetRandomSatisfiedChannel(group, modelName, ignoreFirstPriority)
	}

	req := &channelrouter.RouteRequest{
		Group:               group,
		Model:               modelName,
		UserId:              userId,
		IgnoreFirstPriority: ignoreFirstPriority,
		SessionKey:          sessionKey,
	}

	ch, err := channelrouter.DefaultRouter.Route(c.Request.Context(), req, candidates)
	if err != nil {
		return model.CacheGetRandomSatisfiedChannel(group, modelName, ignoreFirstPriority)
	}
	return ch, nil
}

func SetupContextForSelectedChannel(c *gin.Context, channel *model.Channel, modelName string) {
	c.Set(ctxkey.Channel, channel.Type)
	c.Set(ctxkey.ChannelId, channel.Id)
	c.Set(ctxkey.ChannelName, channel.Name)
	if channel.SystemPrompt != nil && *channel.SystemPrompt != "" {
		c.Set(ctxkey.SystemPrompt, *channel.SystemPrompt)
	}
	c.Set(ctxkey.ModelMapping, channel.GetModelMapping())
	c.Set(ctxkey.OriginalModel, modelName) // for retry
	c.Request.Header.Set("Authorization", fmt.Sprintf("Bearer %s", channel.Key))
	channelID := registry.IDByLegacyType(channel.Type)
	c.Set(ctxkey.ChannelID, channelID)
	baseURL := channel.GetBaseURL()
	if baseURL == "" {
		baseURL = registry.GetDefaultBaseURL(channelID)
	}
	c.Set(ctxkey.BaseURL, baseURL)
	cfg, _ := channel.LoadConfig()
	// this is for backward compatibility
	if channel.Other != nil {
		switch registry.IDByLegacyType(channel.Type) {
		case "azure":
			if cfg.APIVersion == "" {
				cfg.APIVersion = *channel.Other
			}
		case "xunfei":
			if cfg.APIVersion == "" {
				cfg.APIVersion = *channel.Other
			}
		case "gemini":
			if cfg.APIVersion == "" {
				cfg.APIVersion = *channel.Other
			}
		case "aiproxylibrary":
			if cfg.LibraryID == "" {
				cfg.LibraryID = *channel.Other
			}
		case "ali":
			if cfg.Plugin == "" {
				cfg.Plugin = *channel.Other
			}
		}
	}
	c.Set(ctxkey.Config, cfg)
}

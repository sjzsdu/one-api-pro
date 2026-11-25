package middleware

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strconv"

	"github.com/gin-gonic/gin"

	"github.com/modelbus/one-api-pro/channelrouter"
	"github.com/modelbus/one-api-pro/common"
	"github.com/modelbus/one-api-pro/common/config"
	"github.com/modelbus/one-api-pro/common/ctxkey"
	"github.com/modelbus/one-api-pro/common/logger"
	"github.com/modelbus/one-api-pro/model"
	"github.com/modelbus/one-api-pro/modelrouter"
	"github.com/modelbus/one-api-pro/relay/registry"
	schema "github.com/modelbus/one-api-pro/relay/schema"
)

type ModelRequest struct {
	Model string `json:"model" form:"model"`
}

func Distribute() func(c *gin.Context) {
	return func(c *gin.Context) {
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
			if requestModel == "auto" && !isChatAutoPath(c.Request.URL.Path) {
				abortWithMessage(c, http.StatusBadRequest, "auto routing is only supported for chat operations")
				return
			}
			if requestModel == "auto" && config.ModelAutoEnabled && modelrouter.DefaultRouter != nil {
				c.Set(ctxkey.OriginalRequestModel, requestModel)
				var parsed schema.GeneralOpenAIRequest
				var body []byte
				body, _ = common.GetRequestBody(c)
				_ = json.Unmarshal(body, &parsed)
				// For /v1/responses endpoint, parse input field as messages and tools
				if c.Request.URL.Path == "/v1/responses" {
					if len(parsed.Messages) == 0 {
						parsed.Messages = parseResponsesInputAsMessages(body)
					}
					if len(parsed.Tools) == 0 {
						parsed.Tools = parseResponsesTools(body)
					}
				}
				routerRequest := &modelrouter.ModelSelectRequest{
					Model:     requestModel,
					Messages:  parsed.Messages,
					Tools:     parsed.Tools,
					MaxTokens: parsed.MaxTokens,
				}
				selected, err := modelrouter.DefaultRouter.SelectModel(ctx, userGroup, userId, routerRequest)
				if err != nil {
					abortWithMessage(c, http.StatusServiceUnavailable, fmt.Sprintf("model auto 路由失败: %s", err.Error()))
					return
				}
				requestModel = selected
				c.Set(ctxkey.RequestModel, requestModel)
				c.Set(ctxkey.ModelRouterRequest, routerRequest)
				if routerRequest.DisableSessionPin {
					c.Set(ctxkey.DisableSessionPin, true)
				}
			}
			var err error
			channel, err = selectChannelViaRouter(c, userGroup, requestModel, userId, false)
			for err != nil && GetAliasPool(c) != nil {
				advanced, advanceErr := NextAliasPoolTarget(c)
				if advanceErr != nil {
					err = advanceErr
					break
				}
				if !advanced {
					break
				}
				requestModel = c.GetString(ctxkey.RequestModel)
				channel, err = selectChannelViaRouter(c, userGroup, requestModel, userId, false)
			}
			if err != nil {
				var operationErr *operationSelectionError
				if errors.As(err, &operationErr) {
					abortOperationSelection(c, operationErr)
					return
				}
				message := noChannelMessage(userGroup, requestModel, err)
				if channel != nil {
					logger.SysError(fmt.Sprintf("渠道不存在：%d", channel.Id))
					message = "数据库一致性已被破坏，请联系管理员"
				}
				abortWithMessage(c, http.StatusServiceUnavailable, message)
				return
			}
		}
		if capabilityErr := channelOperationError(c, channel, requestModel); capabilityErr != nil {
			abortOperationSelection(c, &operationSelectionError{reason: capabilityErr})
			return
		}
		logger.Debugf(ctx, "user id %d, user group: %s, request model: %s, using channel #%d", userId, userGroup, requestModel, channel.Id)

		sessionKey := ""
		if GetAliasPool(c) == nil && config.ChannelStickySessionEnabled && !c.GetBool(ctxkey.DisableSessionPin) && userId > 0 && requestModel != "" {
			sessionKey = channelrouter.MakeSessionKey(userId, requestModel)
			c.Set(ctxkey.SessionKey, sessionKey)
		}

		if config.ChannelConcurrencyEnabled && channel.GetMaxConcurrency() > 0 {
			if !channelrouter.DefaultRouter.TryAcquireConcurrency(channel.Id, channel.GetMaxConcurrency()) {
				logger.Debugf(ctx, "channel #%d at concurrency limit (%d), trying alternate", channel.Id, channel.GetMaxConcurrency())
				altChannel, altErr := selectChannelViaRouter(c, userGroup, requestModel, userId, false)
				if GetAliasPool(c) != nil {
					if altErr != nil || altChannel == nil || altChannel.Id == channel.Id || !channelrouter.DefaultRouter.TryAcquireConcurrency(altChannel.Id, altChannel.GetMaxConcurrency()) {
						abortWithMessage(c, http.StatusServiceUnavailable, "模型池渠道并发已满，请稍后重试")
						return
					}
					channel = altChannel
				} else if altErr == nil && altChannel.Id != channel.Id {
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

// noChannelMessage 生成「无可用渠道」的对外文案。
// Builds the client-facing message for a failed channel selection.
//
// 「该分组确实没有渠道」和「渠道存在但暂时不可用」是两回事，混成一句话会让
// 上游配额打满（429 → 冷却）被误判成分组没配渠道（2026-11-24 线上事故）。
// 候选渠道存在、但被冷却/并发/限速等条件过滤掉时，这里把真实原因一并写出。
func noChannelMessage(userGroup, requestModel string, err error) string {
	var noChannel *channelrouter.NoAvailableChannelError
	if errors.As(err, &noChannel) {
		if reason := noChannel.Reason(); reason != "" {
			return fmt.Sprintf("当前分组 %s 下模型 %s 暂时无可用渠道（%s）", userGroup, requestModel, reason)
		}
	}
	return fmt.Sprintf("当前分组 %s 下对于模型 %s 无可用渠道", userGroup, requestModel)
}

func selectChannelViaRouter(c *gin.Context, group, modelName string, userId int, ignoreFirstPriority bool) (*model.Channel, error) {
	if GetAliasPool(c) != nil {
		excluded := map[int]bool{}
		var rejected error
		eligible := 0
		for _, ch := range model.GetAliasPoolChannels(group, modelName) {
			if err := channelOperationError(c, ch, modelName); err != nil {
				excluded[ch.Id] = true
				if rejected == nil {
					rejected = err
				}
			} else {
				eligible++
			}
		}
		if eligible == 0 && rejected != nil {
			return nil, &operationSelectionError{reason: rejected}
		}
		return SelectAliasPoolChannel(c, group, modelName, excluded)
	}
	candidates := OperationChannelCandidates(c, group, modelName)
	if len(candidates) == 0 {
		if value, ok := c.Get("gateway.operation_candidate_error"); ok {
			if reason, ok := value.(*operationSelectionError); ok {
				return nil, reason
			}
		}
		return nil, fmt.Errorf("no channel supports requested operation")
	}
	sessionKey := ""
	if config.ChannelStickySessionEnabled && !c.GetBool(ctxkey.DisableSessionPin) && userId > 0 && modelName != "" {
		sessionKey = channelrouter.MakeSessionKey(userId, modelName)
	}
	req := &channelrouter.RouteRequest{Group: group, Model: modelName, UserId: userId, IgnoreFirstPriority: ignoreFirstPriority, SessionKey: sessionKey}
	r := channelrouter.DefaultRouter
	if r == nil {
		r = channelrouter.NewChannelRouter()
	}
	return r.Route(c.Request.Context(), req, candidates)
}

func abortOperationSelection(c *gin.Context, err *operationSelectionError) {
	c.JSON(http.StatusBadRequest, gin.H{"error": gin.H{"type": "one_api_error", "code": "model_operation_unsupported", "message": err.Error()}})
	c.Abort()
}

func SetupContextForSelectedChannel(c *gin.Context, channel *model.Channel, modelName string) {
	c.Set(ctxkey.Channel, channel.Type)
	c.Set(ctxkey.ChannelId, channel.Id)
	c.Set(ctxkey.ChannelName, channel.Name)
	c.Set(ctxkey.SystemPrompt, "")
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

// parseResponsesInputAsMessages extracts messages from Responses API input format
func parseResponsesInputAsMessages(body []byte) []schema.Message {
	var req struct {
		Input        json.RawMessage `json:"input"`
		Instructions string          `json:"instructions,omitempty"`
	}
	if err := json.Unmarshal(body, &req); err != nil {
		return nil
	}

	var messages []schema.Message

	// Add instructions as system message
	if req.Instructions != "" {
		messages = append(messages, schema.Message{
			Role:    "system",
			Content: req.Instructions,
		})
	}

	// Try to parse input as string
	var inputStr string
	if err := json.Unmarshal(req.Input, &inputStr); err == nil && inputStr != "" {
		messages = append(messages, schema.Message{
			Role:    "user",
			Content: inputStr,
		})
		return messages
	}

	// Try to parse input as array
	var inputItems []struct {
		Type    string          `json:"type,omitempty"`
		Role    string          `json:"role,omitempty"`
		Content json.RawMessage `json:"content,omitempty"`
		ID      string          `json:"id,omitempty"`
		Name    string          `json:"name,omitempty"`
		Args    string          `json:"arguments,omitempty"`
		Output  string          `json:"output,omitempty"`
		CallID  string          `json:"call_id,omitempty"`
	}
	if err := json.Unmarshal(req.Input, &inputItems); err != nil {
		return messages
	}

	var toolCalls []schema.Tool
	flushToolCalls := func() {
		if len(toolCalls) == 0 {
			return
		}
		messages = append(messages, schema.Message{Role: "assistant", ToolCalls: toolCalls})
		toolCalls = nil
	}
	for _, item := range inputItems {
		switch item.Type {
		case "function_call":
			callID := item.CallID
			if callID == "" {
				callID = item.ID
			}
			toolCalls = append(toolCalls, schema.Tool{Id: callID, Type: "function", Function: schema.Function{Name: item.Name, Arguments: item.Args}})
		case "function_call_output":
			flushToolCalls()
			messages = append(messages, schema.Message{
				Role:       "tool",
				Content:    item.Output,
				ToolCallId: item.CallID,
			})
		default:
			flushToolCalls()
			role := item.Role
			if role == "" {
				role = "user"
			}
			if role == "system" || role == "developer" {
				role = "system"
			}
			// Extract text from content
			text := extractContentText(item.Content)
			if text == "" {
				continue
			}
			messages = append(messages, schema.Message{Role: role, Content: text})
		}
	}
	flushToolCalls()

	return messages
}

func extractContentText(raw json.RawMessage) string {
	if len(raw) == 0 {
		return ""
	}

	var s string
	if err := json.Unmarshal(raw, &s); err == nil {
		return s
	}

	var blocks []struct {
		Type string `json:"type"`
		Text string `json:"text"`
	}
	if err := json.Unmarshal(raw, &blocks); err != nil {
		return ""
	}

	var result string
	for _, b := range blocks {
		switch b.Type {
		case "input_text", "text", "output_text":
			if b.Text != "" {
				if result != "" {
					result += "\n"
				}
				result += b.Text
			}
		}
	}
	return result
}

// parseResponsesTools extracts tools from a Responses API request body and
// converts them to Chat Completions format for feature detection by the router.
func parseResponsesTools(body []byte) []schema.Tool {
	var req struct {
		Tools json.RawMessage `json:"tools"`
	}
	if err := json.Unmarshal(body, &req); err != nil || len(req.Tools) == 0 {
		return nil
	}
	var items []json.RawMessage
	if err := json.Unmarshal(req.Tools, &items); err != nil {
		return nil
	}
	return flattenResponsesToolsForRouter(items, "")
}

func flattenResponsesToolsForRouter(items []json.RawMessage, namespace string) []schema.Tool {
	var result []schema.Tool
	for _, rawItem := range items {
		var item struct {
			Type        string            `json:"type"`
			Name        string            `json:"name"`
			Description string            `json:"description"`
			Parameters  any               `json:"parameters"`
			Tools       []json.RawMessage `json:"tools"`
		}
		if json.Unmarshal(rawItem, &item) != nil {
			continue
		}
		if item.Type == "namespace" {
			childNS := item.Name
			if childNS == "functions" {
				childNS = ""
			}
			result = append(result, flattenResponsesToolsForRouter(item.Tools, childNS)...)
			continue
		}
		if (item.Type != "function" && item.Type != "custom") || item.Name == "" {
			continue
		}
		alias := item.Name
		if namespace != "" {
			alias = namespace + "__" + item.Name
		}
		result = append(result, schema.Tool{
			Type: "function",
			Function: schema.Function{
				Name:        alias,
				Description: item.Description,
				Parameters:  item.Parameters,
			},
		})
	}
	return result
}

func isChatAutoPath(path string) bool {
	return path == "/v1/chat/completions" || path == "/v1/completions" || path == "/v1/messages" || path == "/v1/responses"
}

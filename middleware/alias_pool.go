// alias_pool.go 模型池的请求选择、会话固定和有限故障切换
// Selects authorized pool candidates and preserves conversation affinity.
// 版本: v0.0.28
// 日期: 2026-11-22
// 作者: Codex
package middleware

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"github.com/gin-gonic/gin"
	"github.com/modelbus/one-api-pro/channelrouter"
	"github.com/modelbus/one-api-pro/common/ctxkey"
	"github.com/modelbus/one-api-pro/common/logger"
	"github.com/modelbus/one-api-pro/model"
	"github.com/modelbus/one-api-pro/modelrouter"
	schema "github.com/modelbus/one-api-pro/relay/schema"
	"strings"
	"time"
)

const aliasPoolContextKey = "alias_pool_state"

// AliasPoolState is the per-request, already-authorized selection snapshot.
// 版本: v0.0.28
// 日期: 2026-11-22
type AliasPoolState struct {
	Alias       model.UserModelAlias
	Targets     []model.UserModelAliasTarget
	Index       int
	UserID      int
	Group       string
	SessionKey  string
	Binding     model.AliasSessionBinding
	GlobalGrant bool
	PrefixScope string
	PrefixHash  string
}

// GetAliasPool returns the request's pool snapshot, if any.
// 版本: v0.0.28
// 日期: 2026-11-22
func GetAliasPool(c *gin.Context) *AliasPoolState {
	value, _ := c.Get(aliasPoolContextKey)
	state, _ := value.(*AliasPoolState)
	return state
}
func aliasCooldownKey(state *AliasPoolState, name string) string {
	return fmt.Sprintf("%s:%d:%s:%s", state.Group, state.Alias.UserId, state.Alias.Alias, name)
}
func allowedAliasTarget(allowed string, item model.UserModelAlias, target string) bool {
	if allowed == "" {
		return true
	}
	for _, name := range strings.Split(allowed, ",") {
		if name == "*" || name == target || (item.Source == "global" && strings.EqualFold(name, item.Alias)) {
			return true
		}
	}
	return false
}
func prepareAliasPool(c *gin.Context, userID int, name string, body []byte) (string, bool, error) {
	items, available, err := model.GetUserModelAliasesWithGroupModels(c.Request.Context(), userID)
	if err != nil {
		return "", false, err
	}
	if available[name] {
		return name, false, nil
	}
	var item *model.UserModelAlias
	for i := range items {
		if items[i].Alias == strings.ToLower(name) {
			item = &items[i]
			break
		}
	}
	if item == nil {
		return name, false, nil
	}
	group, err := model.CacheGetUserGroup(userID)
	if err != nil {
		return "", true, err
	}
	state := &AliasPoolState{Alias: *item, UserID: userID, Group: group}
	allowed := c.GetString(ctxkey.AvailableModels)
	for _, target := range item.Targets {
		if available[target.Model] && allowedAliasTarget(allowed, *item, target.Model) {
			state.Targets = append(state.Targets, target)
		}
	}
	if len(state.Targets) == 0 {
		return "", true, model.ErrUserModelAliasTargetUnavailable
	}
	if len(item.Targets) > 1 {
		path := c.Request.URL.Path
		if path != "/v1/chat/completions" && path != "/v1/completions" && path != "/v1/messages" && path != "/v1/responses" {
			return "", true, errors.New("多模型池暂仅支持聊天、Messages 和 Responses JSON 请求")
		}
		var parsed schema.GeneralOpenAIRequest
		_ = json.Unmarshal(body, &parsed)
		if path == "/v1/responses" {
			parsed.Messages = parseResponsesInputAsMessages(body)
			parsed.Tools = parseResponsesTools(body)
		}
		features := modelrouter.ExtractRequestFeatures(parsed.Messages, parsed.Tools, parsed.MaxTokens)
		candidates, e := modelrouter.ResolveCandidates(c.Request.Context(), group, features)
		if e != nil {
			return "", true, e
		}
		compatible := map[string]bool{}
		for _, m := range candidates.Models {
			compatible[m] = true
		}
		filtered := []model.UserModelAliasTarget{}
		for _, t := range state.Targets {
			if compatible[t.Model] {
				filtered = append(filtered, t)
			}
		}
		state.Targets = filtered
		if len(state.Targets) == 0 {
			return "", true, errors.New("模型池没有满足当前工具、视觉或上下文要求的候选模型")
		}
	}
	var fields map[string]json.RawMessage
	_ = json.Unmarshal(body, &fields)
	opaque := false
	for _, field := range []string{"previous_response_id", "conversation"} {
		if raw := fields[field]; len(raw) > 0 && string(raw) != "null" && string(raw) != "\"\"" {
			opaque = true
		}
	}
	if opaque {
		return "", true, errors.New("模型别名池暂不支持 previous_response_id 或 conversation 供应商会话引用，请发送完整聊天历史")
	}
	sessionID := strings.TrimSpace(c.GetHeader("X-Session-ID"))
	if len(sessionID) > 128 {
		return "", true, errors.New("X-Session-ID 最多 128 字节")
	}
	if sessionID != "" {
		state.SessionKey = model.AliasPoolKey(userID, *item, sessionID)
	} else {
		identity := buildAliasPrefixIdentity(c.Request.URL.Path, body)
		if identity.Current != "" {
			state.PrefixScope = model.AliasPrefixScope(userID, *item, identity.Protocol)
			state.PrefixHash = identity.Current
			state.SessionKey, err = model.FindAliasPrefixSession(state.PrefixScope, identity.Hashes)
			if err != nil {
				return "", true, fmt.Errorf("自动会话索引不可用: %w", err)
			}
			if state.SessionKey == "" {
				state.SessionKey = model.AliasPrefixSessionKey(state.PrefixScope, state.PrefixHash)
			}
		}
	}
	state.Targets = model.WeightedAliasTargets(state.Targets)
	binding, found, e := model.ReadAliasSession(state.SessionKey)
	if e != nil {
		return "", true, fmt.Errorf("会话固定服务不可用: %w", e)
	}
	eligible := []model.UserModelAliasTarget{}
	for _, t := range state.Targets {
		if !model.AliasPoolCooling(aliasCooldownKey(state, t.Model)) {
			eligible = append(eligible, t)
		}
	}
	if found {
		for i, t := range eligible {
			if t.Model == binding.Model {
				eligible = append([]model.UserModelAliasTarget{t}, append(eligible[:i], eligible[i+1:]...)...)
				break
			}
		}
	}
	state.Targets = eligible
	if len(eligible) == 0 {
		return "", true, model.ErrAliasPoolCooling
	}
	proposed := model.AliasSessionBinding{Model: eligible[0].Model}
	if found {
		state.Binding = binding
		if binding.Model != proposed.Model {
			if e = model.UpdateAliasSession(state.SessionKey, binding.Model, proposed, time.Duration(item.SessionTTLSeconds)*time.Second); e != nil {
				return "", true, e
			}
			state.Binding = proposed
		}
	} else {
		winner, e := model.ClaimAliasSession(state.SessionKey, proposed, time.Duration(item.SessionTTLSeconds)*time.Second)
		if e != nil {
			return "", true, e
		}
		state.Binding = winner
		matched := false
		for i, t := range state.Targets {
			if t.Model == winner.Model {
				state.Targets = append([]model.UserModelAliasTarget{t}, append(state.Targets[:i], state.Targets[i+1:]...)...)
				matched = true
				break
			}
		}
		if !matched {
			return "", true, errors.New("并发会话绑定模型已失去当前令牌权限，请重试")
		}
	}
	if found && state.SessionKey != "" {
		winner, present, e := model.ReadAliasSession(state.SessionKey)
		if e != nil {
			return "", true, e
		}
		if !present {
			winner, e = model.ClaimAliasSession(state.SessionKey, proposed, time.Duration(item.SessionTTLSeconds)*time.Second)
			if e != nil {
				return "", true, e
			}
		}
		matched := false
		for i, t := range state.Targets {
			if t.Model == winner.Model {
				state.Targets[0], state.Targets[i] = state.Targets[i], state.Targets[0]
				matched = true
				break
			}
		}
		if !matched {
			return "", true, model.ErrUserModelAliasTargetUnavailable
		}
		state.Binding = winner
	}
	state.GlobalGrant = item.Source == "global" && allowedAliasTarget(allowed, *item, state.Targets[0].Model)
	c.Set(aliasPoolContextKey, state)
	c.Set(ctxkey.UserModelAlias, name)
	c.Request = c.Request.WithContext(context.WithValue(c.Request.Context(), ctxkey.UserModelAlias, name))
	return state.Targets[0].Model, true, nil
}

// NextAliasPoolTarget advances to one not-yet-attempted candidate and recomputes plan billing.
// 版本: v0.0.28
// 日期: 2026-11-22
func NextAliasPoolTarget(c *gin.Context) (bool, error) {
	state := GetAliasPool(c)
	if state == nil || state.Alias.FailoverEnabled == nil || !*state.Alias.FailoverEnabled || c.Writer.Written() || c.Request.Context().Err() != nil {
		return false, nil
	}
	old := state.Targets[state.Index].Model
	model.CoolAliasPoolCandidate(aliasCooldownKey(state, old))
	state.Index++
	if state.Index >= len(state.Targets) {
		return false, nil
	}
	next := state.Targets[state.Index].Model
	if err := RewriteAliasRequestModel(c, next); err != nil {
		return false, err
	}
	state.Binding = model.AliasSessionBinding{Model: next}
	if err := model.UpdateAliasSession(state.SessionKey, old, state.Binding, time.Duration(state.Alias.SessionTTLSeconds)*time.Second); err != nil {
		return false, err
	}
	if state.SessionKey != "" {
		winner, found, e := model.ReadAliasSession(state.SessionKey)
		if e != nil {
			return false, e
		}
		if !found {
			winner, e = model.ClaimAliasSession(state.SessionKey, state.Binding, time.Duration(state.Alias.SessionTTLSeconds)*time.Second)
			if e != nil {
				return false, e
			}
		}
		if winner.Model != next {
			matched := false
			for i := state.Index; i < len(state.Targets); i++ {
				if state.Targets[i].Model == winner.Model {
					state.Targets[state.Index], state.Targets[i] = state.Targets[i], state.Targets[state.Index]
					matched = true
					break
				}
			}
			if !matched {
				return false, errors.New("并发会话已切换到当前请求不可用的模型，请重试")
			}
			next = winner.Model
			state.Binding = winner
			if e = RewriteAliasRequestModel(c, next); e != nil {
				return false, e
			}
		}
	}
	c.Set(ctxkey.PlanId, 0)
	c.Set(ctxkey.BillingType, "")
	result, err := model.CheckPlanQuota(state.UserID, next)
	if err == nil && result.Usable {
		c.Set(ctxkey.PlanId, result.PlanId)
		c.Set(ctxkey.BillingType, result.BillingType)
	}
	return true, nil
}

// CompleteAliasPool records the successful channel without undoing concurrent failover.
// 版本: v0.0.28
// 日期: 2026-11-22
func CompleteAliasPool(c *gin.Context) {
	state := GetAliasPool(c)
	if state == nil {
		return
	}
	name := c.GetString(ctxkey.RequestModel)
	ttl := time.Duration(state.Alias.SessionTTLSeconds) * time.Second
	if err := model.UpdateAliasSession(state.SessionKey, name, model.AliasSessionBinding{Model: name, ChannelID: c.GetInt(ctxkey.ChannelId)}, ttl); err != nil {
		logger.Warnf(c.Request.Context(), "model pool session update failed: %v", err)
		return
	}
	if err := model.RememberAliasPrefix(state.PrefixScope, state.PrefixHash, state.SessionKey, ttl); err != nil {
		logger.Warnf(c.Request.Context(), "model pool prefix registration failed: %v", err)
	}
}

// SelectAliasPoolChannel selects an untried healthy exact channel, preferring the session's successful binding.
// 版本: v0.0.28
// 日期: 2026-11-22
func SelectAliasPoolChannel(c *gin.Context, group, name string, excluded map[int]bool) (*model.Channel, error) {
	state := GetAliasPool(c)
	channels := model.GetAliasPoolChannels(group, name)
	remaining := []*model.Channel{}
	for _, ch := range channels {
		if channelOperationError(c, ch, name) != nil {
			continue
		}
		if excluded[ch.Id] {
			continue
		}
		if state != nil && state.Binding.Model == name && state.Binding.ChannelID == ch.Id && len(excluded) == 0 {
			if channelrouter.DefaultRouter == nil {
				return ch, nil
			}
			if _, e := channelrouter.DefaultRouter.Route(c.Request.Context(), &channelrouter.RouteRequest{Group: group, Model: name, UserId: state.UserID}, []*model.Channel{ch}); e == nil {
				return ch, nil
			}
		}
		remaining = append(remaining, ch)
	}
	if channelrouter.DefaultRouter != nil {
		return channelrouter.DefaultRouter.Route(c.Request.Context(), &channelrouter.RouteRequest{Group: group, Model: name, UserId: state.UserID}, remaining)
	}
	if len(remaining) == 0 {
		return nil, errors.New("模型池候选没有可用渠道")
	}
	best := remaining[0]
	for _, ch := range remaining {
		if ch.GetPriority() > best.GetPriority() {
			best = ch
		}
	}
	return best, nil
}

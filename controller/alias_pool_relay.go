// alias_pool_relay.go 模型池的有界转发与跨模型故障切换
// Retries channels before switching authorized pool models without replaying output.
// 版本: v0.0.28
// 日期: 2026-11-22
// 作者: Codex
package controller

import (
	"bytes"
	"github.com/gin-gonic/gin"
	"github.com/modelbus/one-api-pro/channelrouter"
	"github.com/modelbus/one-api-pro/common"
	"github.com/modelbus/one-api-pro/common/config"
	"github.com/modelbus/one-api-pro/common/ctxkey"
	"github.com/modelbus/one-api-pro/common/helper"
	"github.com/modelbus/one-api-pro/middleware"
	"github.com/modelbus/one-api-pro/monitor"
	schema "github.com/modelbus/one-api-pro/relay/schema"
	"io"
	"net/http"
	"strings"
)

func retryableAliasFailure(err *schema.ErrorWithStatusCode) bool {
	if err == nil || schema.ExecutionMayHaveSucceeded(err) {
		return false
	}
	code := err.Error.Code
	if text, ok := code.(string); ok && strings.Contains(text, "quota") {
		return false
	}
	if code == "quota_rollback_failed" || code == "convert_request_failed" || code == "model_price_not_found" || code == "insufficient_user_quota" || code == "insufficient_quota" {
		return false
	}
	return err.StatusCode == 429 || err.StatusCode == 529 || err.StatusCode >= 500
}

func relayAliasPool(c *gin.Context, mode int) {
	if aliasResponseStarted(c) {
		return
	}
	state := middleware.GetAliasPool(c)
	var failure *schema.ErrorWithStatusCode
	for {
		name := c.GetString(ctxkey.RequestModel)
		attempted := map[int]bool{}
		retries := config.RetryTimes
		if retries < 1 {
			retries = 1
		}
		if retries > 8 {
			retries = 8
		}
		for attempt := 0; attempt <= retries; attempt++ {
			channelID := c.GetInt(ctxkey.ChannelId)
			if attempted[channelID] {
				break
			}
			attempted[channelID] = true
			body, err := common.GetRequestBody(c)
			if err != nil {
				break
			}
			c.Request.Body = io.NopCloser(bytes.NewReader(body))
			failure = relayHelper(c, mode)
			if c.GetBool(ctxkey.ConcurrencyAcquired) && channelrouter.DefaultRouter != nil {
				channelrouter.DefaultRouter.ReleaseConcurrency(channelID)
				c.Set(ctxkey.ConcurrencyAcquired, false)
			}
			if failure == nil {
				monitor.Emit(channelID, true)
				middleware.CompleteAliasPool(c)
				return
			}
			monitor.Emit(channelID, false)
			if aliasResponseStarted(c) || c.Request.Context().Err() != nil {
				return
			}
			if schema.ExecutionMayHaveSucceeded(failure) {
				c.Header("x-should-retry", "false")
			}
			if !retryableAliasFailure(failure) {
				break
			}
			if attempt == retries {
				break
			}
			next, e := middleware.SelectAliasPoolChannel(c, state.Group, name, attempted)
			if e != nil || next == nil || attempted[next.Id] {
				break
			}
			if config.ChannelConcurrencyEnabled && channelrouter.DefaultRouter != nil && next.GetMaxConcurrency() > 0 {
				if !channelrouter.DefaultRouter.TryAcquireConcurrency(next.Id, next.GetMaxConcurrency()) {
					break
				}
				c.Set(ctxkey.ConcurrencyAcquired, true)
			}
			middleware.SetupContextForSelectedChannel(c, next, name)
			if channelrouter.DefaultRouter != nil && next.GetRPM() > 0 {
				channelrouter.DefaultRouter.IncrementRPM(next.Id)
			}
		}
		if failure == nil {
			failure = &schema.ErrorWithStatusCode{StatusCode: http.StatusServiceUnavailable, Error: schema.Error{Message: "模型池没有可用渠道", Type: "server_error", Code: "alias_pool_unavailable"}}
		}
		if schema.ExecutionMayHaveSucceeded(failure) {
			c.Header("x-should-retry", "false")
		}
		if !retryableAliasFailure(failure) {
			break
		}
		advanced, err := middleware.NextAliasPoolTarget(c)
		if err != nil || !advanced {
			break
		}
		name = c.GetString(ctxkey.RequestModel)
		// A candidate with no channel is exhausted before any request can be sent.
		found := false
		for {
			next, e := middleware.SelectAliasPoolChannel(c, state.Group, name, nil)
			if e == nil && next != nil {
				if config.ChannelConcurrencyEnabled && channelrouter.DefaultRouter != nil && next.GetMaxConcurrency() > 0 {
					if !channelrouter.DefaultRouter.TryAcquireConcurrency(next.Id, next.GetMaxConcurrency()) {
						advanced, e = middleware.NextAliasPoolTarget(c)
						if e != nil || !advanced {
							break
						}
						name = c.GetString(ctxkey.RequestModel)
						continue
					}
					c.Set(ctxkey.ConcurrencyAcquired, true)
				}
				middleware.SetupContextForSelectedChannel(c, next, name)
				if channelrouter.DefaultRouter != nil && next.GetRPM() > 0 {
					channelrouter.DefaultRouter.IncrementRPM(next.Id)
				}
				found = true
				break
			}
			advanced, e = middleware.NextAliasPoolTarget(c)
			if e != nil || !advanced {
				break
			}
			name = c.GetString(ctxkey.RequestModel)
		}
		if !found {
			break
		}
	}
	if aliasResponseStarted(c) {
		return
	}
	failure.Error.Message = helper.MessageWithRequestId(failure.Error.Message, c.GetString(helper.RequestIdKey))
	c.JSON(failure.StatusCode, gin.H{"error": failure.Error})
}

func aliasResponseStarted(c *gin.Context) bool {
	if c.Writer.Written() {
		return true
	}
	switch w := c.Writer.(type) {
	case *claudeMessagesWriter:
		return w.buffer.Len() > 0 || w.pending != "" || w.written || w.state.started
	case *responsesJSONWriter:
		return w.buffer.Len() > 0 || w.written
	case *responsesStreamWriter:
		return w.started
	}
	return false
}

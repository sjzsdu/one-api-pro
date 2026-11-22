// alias_pool_guard_test.go 模型池流式输出及包装器重试边界测试
// Prevents replay once a response or protocol wrapper has consumed output.
// 版本: v0.0.28
// 日期: 2026-11-22
// 作者: Codex
package controller

import (
	"github.com/gin-gonic/gin"
	schema "github.com/modelbus/one-api-pro/relay/schema"
	"net/http/httptest"
	"testing"
)

// TestAliasPool_OutputGuard verifies the model-pool behavior described by this test.
// 版本: v0.0.28
// 日期: 2026-11-22
func TestAliasPool_OutputGuard(t *testing.T) {
	for _, kind := range []string{"plain", "claude-buffer", "responses-buffer", "responses-stream"} {
		t.Run(kind, func(t *testing.T) {
			c, _ := gin.CreateTestContext(httptest.NewRecorder())
			switch kind {
			case "plain":
				c.Writer.Write([]byte("data: partial\n\n"))
			case "claude-buffer":
				w := &claudeMessagesWriter{ResponseWriter: c.Writer}
				w.buffer.WriteString("partial")
				c.Writer = w
			case "responses-buffer":
				w := newResponsesJSONWriter(c.Writer, "a", nil)
				w.buffer.WriteString("partial")
				c.Writer = w
			case "responses-stream":
				w := newResponsesStreamWriter(c.Writer, "a", nil)
				w.started = true
				c.Writer = w
			}
			if !aliasResponseStarted(c) {
				t.Fatal("started output can be replayed")
			}
			relayAliasPool(c, 0)
		})
	}
	if retryableAliasFailure(&schema.ErrorWithStatusCode{StatusCode: 400}) {
		t.Fatal("invalid request retried")
	}
	if retryableAliasFailure(&schema.ErrorWithStatusCode{StatusCode: 500, Error: schema.Error{Code: "quota_rollback_failed"}}) {
		t.Fatal("failed rollback retried")
	}
	if !retryableAliasFailure(&schema.ErrorWithStatusCode{StatusCode: 429}) {
		t.Fatal("rate limit should fail over")
	}
}

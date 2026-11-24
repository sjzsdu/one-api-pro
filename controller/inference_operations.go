// inference_operations.go 强类型推理操作HTTP入口
// Exposes operation-specific relays while preserving legacy compatible audio.
// 版本: v0.0.28
// 日期: 2026-11-23
// 作者: Codex
package controller

import (
	"github.com/gin-gonic/gin"
	"github.com/modelbus/one-api-pro/common/ctxkey"
	"github.com/modelbus/one-api-pro/model"
	"github.com/modelbus/one-api-pro/relay"
	"github.com/modelbus/one-api-pro/relay/adaptor"
	handler "github.com/modelbus/one-api-pro/relay/handler"
	"github.com/modelbus/one-api-pro/relay/meta"
	schema "github.com/modelbus/one-api-pro/relay/schema"
)

// RelayRerank executes native document ranking.
// 版本: v0.0.28
// 日期: 2026-11-23
func RelayRerank(c *gin.Context) {
	writeOperationError(c, handler.RelayOperationHelper(c, "rerank.score"))
}

// RelayImageEdit executes supported image editing instead of a chat request.
// 版本: v0.0.28
// 日期: 2026-11-23
func RelayImageEdit(c *gin.Context) {
	writeOperationError(c, handler.RelayOperationHelper(c, "image.edit"))
}

// RelayTranscription keeps legacy audio passthrough and enables Ali's typed native ASR.
// 版本: v0.0.28
// 日期: 2026-11-23
func RelayTranscription(c *gin.Context) {
	m := meta.GetByContext(c)
	actual := m.OriginModelName
	if mapped := m.ModelMapping[actual]; mapped != "" {
		actual = mapped
	}
	if !typedTranscriptionModel(c.GetString(ctxkey.ChannelID), actual) {
		Relay(c)
		return
	}
	writeOperationError(c, handler.RelayOperationHelper(c, "audio.transcribe"))
}

func typedTranscriptionModel(provider, actual string) bool {
	_, supportsNative := relay.GetAdaptorByChannelID(provider).(adaptor.Transcriber)
	return supportsNative && provider == "ali" && model.AliOperationModelFamily(actual) == "qwen3-asr-flash" && containsModelCapability(relay.RuntimeOperationModes(provider, actual)["audio.transcribe"], "sync")
}

func writeOperationError(c *gin.Context, err *schema.ErrorWithStatusCode) {
	if err != nil && !c.Writer.Written() {
		if schema.ExecutionMayHaveSucceeded(err) {
			c.Header("x-should-retry", "false")
		}
		c.JSON(err.StatusCode, gin.H{"error": err.Error})
	}
}

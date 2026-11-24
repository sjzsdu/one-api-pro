// inference_body_limit.go 新媒体操作请求体资源上限
// Bounds typed operation and job inputs before authentication parses their bodies.
// 版本: v0.0.28
// 日期: 2026-11-23
// 作者: Codex
package middleware

import (
	"github.com/gin-gonic/gin"
	"net/http"
)

// InferenceBodyLimit protects newly introduced media routes without changing legacy chat limits.
// 版本: v0.0.28
// 日期: 2026-11-23
func InferenceBodyLimit() gin.HandlerFunc {
	return func(c *gin.Context) {
		switch c.Request.URL.Path {
		case "/v1/rerank", "/v1/media/jobs", "/v1/images/edits", "/v1/audio/transcriptions":
			c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, 32<<20)
		}
		c.Next()
	}
}

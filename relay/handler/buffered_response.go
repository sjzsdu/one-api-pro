// buffered_response.go 同步媒体响应提交前缓冲
// Buffers bounded provider output so response validation and settlement precede HTTP commitment.
// 版本: v0.0.28
// 日期: 2026-11-23
// 作者: Codex
package controller

import (
	"bytes"
	"github.com/gin-gonic/gin"
	"io"
	"net/http"
)

type bufferedInferenceResponse struct {
	gin.ResponseWriter
	body    bytes.Buffer
	header  http.Header
	status  int
	written bool
}

func newBufferedInferenceResponse(w gin.ResponseWriter) *bufferedInferenceResponse {
	return &bufferedInferenceResponse{ResponseWriter: w, header: w.Header().Clone(), status: 200}
}
func (w *bufferedInferenceResponse) Header() http.Header { return w.header }
func (w *bufferedInferenceResponse) WriteHeader(code int) {
	if !w.written {
		w.status = code
	}
}
func (w *bufferedInferenceResponse) WriteHeaderNow() { w.written = true }
func (w *bufferedInferenceResponse) Write(p []byte) (int, error) {
	if w.body.Len()+len(p) > 64<<20 {
		return 0, io.ErrShortBuffer
	}
	w.written = true
	return w.body.Write(p)
}
func (w *bufferedInferenceResponse) WriteString(s string) (int, error) { return w.Write([]byte(s)) }
func (w *bufferedInferenceResponse) Written() bool                     { return w.written }
func (w *bufferedInferenceResponse) Status() int                       { return w.status }
func (w *bufferedInferenceResponse) Size() int                         { return w.body.Len() }
func (w *bufferedInferenceResponse) Flush()                            {}

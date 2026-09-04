package middleware

import (
	"bytes"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/modelbus/one-api-pro/common"
	"github.com/modelbus/one-api-pro/common/ctxkey"
)

func TestFilterAvailableModelsHonorsTokenRestriction(t *testing.T) {
	models := filterAvailableModels(
		[]string{"gpt-4o", "auto", "deepseek-chat", "gpt-4o"},
		"deepseek-chat, claude-3",
	)
	assert.Equal(t, []string{"deepseek-chat"}, models)
}

func TestRewriteRequestModelUpdatesReusableBody(t *testing.T) {
	gin.SetMode(gin.TestMode)
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	c.Request = httptest.NewRequest("POST", "/v1/chat/completions", bytes.NewBufferString(`{"model":"auto","messages":[{"role":"user","content":"hello"}]}`))
	c.Request.Header.Set("Content-Type", "application/json")

	require.NoError(t, rewriteRequestModel(c, "gpt-4o"))
	body, err := common.GetRequestBody(c)
	require.NoError(t, err)
	assert.JSONEq(t, `{"model":"gpt-4o","messages":[{"role":"user","content":"hello"}]}`, string(body))
	assert.Equal(t, body, c.MustGet(ctxkey.KeyRequestBody))
}

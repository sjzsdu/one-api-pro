// user_model_alias.go 在权限校验前解析用户模型别名
// Resolves aliases once and rewrites reusable JSON bodies before authorization.
// 版本: v0.0.28
// 日期: 2026-11-22
// 作者: Codex
package middleware

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"mime"

	"github.com/gin-gonic/gin"
	"github.com/modelbus/one-api-pro/common"
	"github.com/modelbus/one-api-pro/common/ctxkey"
)

// resolveUserModelAlias fixes the target for the lifetime of one request.
// 版本: v0.0.28
// 日期: 2026-11-22
func resolveUserModelAlias(c *gin.Context, userId int, name string) (string, error) {
	contentType, _, _ := mime.ParseMediaType(c.GetHeader("Content-Type"))
	if contentType == "multipart/form-data" || contentType == "application/x-www-form-urlencoded" {
		if formModel := c.PostForm("model"); formModel != "" {
			name = formModel
		}
	}
	if name == "" {
		return name, nil
	}
	body, _ := common.GetRequestBody(c)
	target, isAlias, err := prepareAliasPool(c, userId, name, body)
	if err != nil || !isAlias {
		return target, err
	}
	if contentType != "application/json" {
		if contentType == "multipart/form-data" {
			return "", errors.New("模型别名暂仅支持 application/json 请求，不支持 multipart 音频上传")
		}
		if contentType == "" {
			contentType = "未提供 Content-Type"
		}
		return "", fmt.Errorf("模型别名暂仅支持 application/json 请求，当前请求格式为 %s，请设置 Content-Type: application/json 并发送 JSON 请求体", contentType)
	}
	return target, RewriteAliasRequestModel(c, target)
}

// RewriteAliasRequestModel synchronizes the JSON body and all routing model fields.
// 版本: v0.0.28
// 日期: 2026-11-22
func RewriteAliasRequestModel(c *gin.Context, target string) error {
	body, err := common.GetRequestBody(c)

	if err != nil {
		return err
	}
	var fields map[string]json.RawMessage
	if err = json.Unmarshal(body, &fields); err != nil {
		return err
	}
	if fields == nil {
		return errors.New("模型别名请求体必须是 JSON object，不能为 null")
	}
	fields["model"], _ = json.Marshal(target)
	rewritten, err := json.Marshal(fields)
	if err != nil {
		return err
	}
	c.Set(ctxkey.KeyRequestBody, rewritten)
	c.Request.Body = io.NopCloser(bytes.NewReader(rewritten))
	c.Request.ContentLength = int64(len(rewritten))
	for i := range c.Params {
		if c.Params[i].Key == "model" {
			c.Params[i].Value = target
		}
	}
	c.Set(ctxkey.RequestModel, target)
	return nil
}

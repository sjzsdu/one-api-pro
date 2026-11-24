// wan_image.go 万相 2.7 同步图片协议转换
// Adapts Wan 2.7 synchronous message-based image generation.
// 版本: v0.0.28
// 日期: 2026-11-23
// 作者: Codex
package ali

import (
	"encoding/base64"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"
	"github.com/modelbus/one-api-pro/common/helper"
	dbmodel "github.com/modelbus/one-api-pro/model"
	"github.com/modelbus/one-api-pro/relay/adaptor/openai"
	schema "github.com/modelbus/one-api-pro/relay/schema"
)

func isWan27(model string) bool {
	family := dbmodel.AliOperationModelFamily(model)
	return family == "wan2.7-image" || family == "wan2.7-image-pro"
}

func convertWanImageRequest(request *schema.ImageRequest) (any, error) {
	if request.N < 1 || request.N > 4 {
		return nil, errors.New("Wan image n must be between 1 and 4")
	}
	if request.Seed != nil && (*request.Seed < 0 || *request.Seed > 2147483647) {
		return nil, errors.New("seed must be between 0 and 2147483647")
	}
	if request.ResponseFormat != "" && request.ResponseFormat != "url" && request.ResponseFormat != "b64_json" {
		return nil, errors.New("unsupported image response_format")
	}
	// Wan 2.7 does not accept negative_prompt; retain the positive prompt verbatim.
	return map[string]any{
		"model": request.Model,
		"input": map[string]any{"messages": []any{map[string]any{"role": "user", "content": []any{map[string]any{"text": request.Prompt}}}}},
		"parameters": struct {
			Size string `json:"size"`
			N    int    `json:"n"`
			Seed *int64 `json:"seed,omitempty"`
		}{strings.ReplaceAll(request.Size, "x", "*"), request.N, request.Seed},
	}, nil
}

func wanImageHandler(c *gin.Context, resp *http.Response) (*schema.ErrorWithStatusCode, *schema.Usage) {
	defer resp.Body.Close()
	var native struct {
		Code   string `json:"code"`
		Output struct {
			Choices []struct {
				Message struct {
					Content []struct {
						Image string `json:"image"`
					} `json:"content"`
				} `json:"message"`
			} `json:"choices"`
		} `json:"output"`
	}
	raw, err := io.ReadAll(io.LimitReader(resp.Body, (64<<20)+1))
	if err != nil || len(raw) > 64<<20 || json.Unmarshal(raw, &native) != nil {
		return openai.ErrorWrapper(errors.New("invalid Wan image JSON"), "unmarshal_response_body_failed", http.StatusBadGateway), nil
	}
	if native.Code != "" {
		return openai.ErrorWrapper(errors.New("Wan image upstream error: "+native.Code), "wan_image_error", http.StatusBadGateway), nil
	}
	result := openai.ImageResponse{Created: helper.GetTimestamp(), Data: make([]openai.ImageData, 0)}
	for _, choice := range native.Output.Choices {
		for _, item := range choice.Message.Content {
			if item.Image == "" {
				continue
			}
			image := openai.ImageData{Url: item.Image}
			if c.GetString("response_format") == "b64_json" {
				data, _, err := downloadMedia(c.Request.Context(), item.Image)
				if err != nil {
					return openai.ErrorWrapper(err, "image_download_failed", http.StatusBadGateway), nil
				}
				image.Url = ""
				image.B64Json = base64.StdEncoding.EncodeToString(data)
			}
			result.Data = append(result.Data, image)
		}
	}
	if len(result.Data) == 0 {
		return openai.ErrorWrapper(errors.New("Wan returned no images"), "image_result_missing", http.StatusBadGateway), nil
	}
	c.Header("Content-Type", "application/json")
	data, err := json.Marshal(result)
	if err != nil {
		return openai.ErrorWrapper(err, "marshal_image_response_failed", http.StatusInternalServerError), nil
	}
	if _, err = c.Writer.Write(data); err != nil {
		return openai.ErrorWrapper(err, "write_image_response_failed", http.StatusInternalServerError), nil
	}
	return nil, nil
}

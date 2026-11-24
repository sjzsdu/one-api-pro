// video.go 万相视频异步提交、查询与取消
// Keeps task operations on the bound workspace and distinguishes unknown submission outcomes.
// 版本: v0.0.28
// 日期: 2026-11-23
// 作者: Codex
package ali

import (
	"context"
	"encoding/json"
	"errors"
	dbmodel "github.com/modelbus/one-api-pro/model"
	"github.com/modelbus/one-api-pro/relay/meta"
	schema "github.com/modelbus/one-api-pro/relay/schema"
	"math/big"
	"net/http"
	"net/url"
)

// SubmitVideo submits one native Wan video intent; errors may have an unknown external outcome.
// 版本: v0.0.28
// 日期: 2026-11-23
func (a *Adaptor) SubmitVideo(ctx context.Context, m *meta.Meta, r *schema.VideoRequest) (*schema.VideoTask, error) {
	if dbmodel.AliOperationModelFamily(r.Model) != "wan2.6-t2v" || r.Prompt == "" || r.Duration < 2 || r.Duration > 15 {
		return nil, errors.New("unsupported video model, prompt or duration")
	}
	input := map[string]any{"prompt": r.Prompt}
	if r.NegativePrompt != "" {
		input["negative_prompt"] = r.NegativePrompt
	}
	parameters := map[string]any{"size": r.Size, "duration": r.Duration}
	if r.Seed != nil {
		parameters["seed"] = r.Seed
	}
	var v struct {
		Output struct {
			ID     string `json:"task_id"`
			Status string `json:"task_status"`
		} `json:"output"`
	}
	err := nativeJSON(ctx, m, "POST", "/api/v1/services/aigc/video-generation/video-synthesis", map[string]any{"model": r.Model, "input": input, "parameters": parameters}, true, &v)
	if err != nil {
		var status *nativeStatusError
		if errors.As(err, &status) && status.ExecutionRejected() {
			return nil, err
		}
		return nil, &schema.SubmissionUnknownError{}
	}
	if v.Output.ID == "" {
		return nil, &schema.SubmissionUnknownError{}
	}
	return &schema.VideoTask{ID: v.Output.ID, State: v.Output.Status}, nil
}

// PollVideo only observes the original provider task and does not trigger generation retries.
// 版本: v0.0.28
// 日期: 2026-11-23
func (a *Adaptor) PollVideo(ctx context.Context, m *meta.Meta, id string) (*schema.VideoTask, error) {
	var v struct {
		Output struct {
			ID    string `json:"task_id"`
			State string `json:"task_status"`
			URL   string `json:"video_url"`
			Code  string `json:"code"`
		} `json:"output"`
		Usage struct {
			Duration       json.Number `json:"duration"`
			OutputDuration json.Number `json:"output_video_duration"`
		} `json:"usage"`
	}
	if err := nativeJSON(ctx, m, http.MethodGet, "/api/v1/tasks/"+url.PathEscape(id), nil, false, &v); err != nil {
		return nil, err
	}
	t := &schema.VideoTask{ID: id, State: v.Output.State, URL: v.Output.URL, ErrorCode: v.Output.Code}
	if t.State == "SUCCEEDED" {
		duration := v.Usage.Duration.String()
		if duration == "" {
			duration = v.Usage.OutputDuration.String()
		}
		r, ok := new(big.Rat).SetString(duration)
		if ok && r.Sign() > 0 {
			r.Mul(r, big.NewRat(1000, 1))
			ms := new(big.Int).Quo(r.Num(), r.Denom())
			if ms.IsInt64() {
				t.Usage = []schema.UsageMetric{{Metric: "video.generated_duration", Quantity: ms.Int64(), Scale: 1000, Unit: "second"}}
			}
		}
	}
	return t, nil
}

// CancelVideo requests cancellation; callers must poll for CANCELED before releasing quota.
// 版本: v0.0.28
// 日期: 2026-11-23
func (a *Adaptor) CancelVideo(ctx context.Context, m *meta.Meta, id string) error {
	var response map[string]any
	return nativeJSON(ctx, m, "POST", "/api/v1/tasks/"+url.PathEscape(id)+"/cancel", map[string]any{}, false, &response)
}

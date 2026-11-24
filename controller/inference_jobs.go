// inference_jobs.go 异步视频任务与私有资产HTTP接口
// Exposes token-owned idempotent video jobs, cancellation and authenticated media delivery.
// 版本: v0.0.28
// 日期: 2026-11-23
// 作者: Codex
package controller

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"github.com/gin-gonic/gin"
	"github.com/modelbus/one-api-pro/common"
	"github.com/modelbus/one-api-pro/common/config"
	"github.com/modelbus/one-api-pro/common/ctxkey"
	"github.com/modelbus/one-api-pro/common/random"
	"github.com/modelbus/one-api-pro/model"
	"github.com/modelbus/one-api-pro/relay"
	"github.com/modelbus/one-api-pro/relay/adaptor"
	ratio "github.com/modelbus/one-api-pro/relay/billing/ratio"
	handler "github.com/modelbus/one-api-pro/relay/handler"
	"github.com/modelbus/one-api-pro/relay/meta"
	schema "github.com/modelbus/one-api-pro/relay/schema"
	"gorm.io/gorm"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"
)

type inferencePriceSnapshot struct {
	Price    ratio.PriceResult `json:"price"`
	Discount float64           `json:"discount"`
}

// ReplayMediaJob serves an authenticated duplicate before new channel or balance admission.
// 版本: v0.0.28
// 日期: 2026-11-23
func ReplayMediaJob(c *gin.Context) {
	key := strings.TrimSpace(c.GetHeader("Idempotency-Key"))
	if key == "" {
		c.Next()
		return
	}
	var old model.InferenceJob
	err := model.DB.Where("user_id = ? AND idempotency_key = ?", c.GetInt(ctxkey.Id), key).First(&old).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		c.Next()
		return
	}
	if err != nil {
		jobError(c, 503, "job_storage_unavailable")
		c.Abort()
		return
	}
	var r schema.VideoRequest
	if common.UnmarshalBodyReusable(c, &r) != nil {
		jobError(c, 400, "invalid_json")
		c.Abort()
		return
	}
	if r.Operation == "" {
		r.Operation = "video.generate"
	}
	if r.Duration == 0 {
		r.Duration = 5
	}
	if r.Size == "" {
		r.Size = "1280*720"
	}
	raw, _ := json.Marshal(r)
	hash := sha256.Sum256(raw)
	if old.TokenID != c.GetInt(ctxkey.TokenId) || old.RequestHash != hex.EncodeToString(hash[:]) {
		jobError(c, 409, "idempotency_conflict")
		c.Abort()
		return
	}
	c.JSON(202, old)
	c.Abort()
}

// CreateMediaJob accepts one durable generation intent and never resubmits an idempotent replay.
// 版本: v0.0.28
// 日期: 2026-11-23
func CreateMediaJob(c *gin.Context) {
	var r schema.VideoRequest
	if err := common.UnmarshalBodyReusable(c, &r); err != nil {
		jobError(c, 400, "invalid_json")
		return
	}
	if r.Operation == "" {
		r.Operation = "video.generate"
	}
	if r.Duration == 0 {
		r.Duration = 5
	}
	if r.Size == "" {
		r.Size = "1280*720"
	}
	if r.Operation != "video.generate" || strings.TrimSpace(r.Prompt) == "" || len(r.Prompt) > 20000 || r.Duration < 2 || r.Duration > 15 || r.Size != "1280*720" && r.Size != "720*1280" && r.Size != "1920*1080" && r.Size != "1080*1920" || r.Seed != nil && (*r.Seed < 0 || *r.Seed > 2147483647) {
		jobError(c, 400, "invalid_video_request")
		return
	}
	key := strings.TrimSpace(c.GetHeader("Idempotency-Key"))
	if key == "" || len(key) > 128 {
		jobError(c, 400, "idempotency_key_required")
		return
	}
	m := meta.GetByContext(c)
	public := c.GetString(ctxkey.RequestModel)
	actual := public
	if mapped := m.ModelMapping[public]; mapped != "" {
		actual = mapped
	}
	m.ActualModelName = actual
	a := relay.GetAdaptorByChannelID(m.ChannelID)
	if _, ok := a.(adaptor.VideoJobProvider); !ok || model.AliOperationModelFamily(actual) != "wan2.6-t2v" {
		jobError(c, 400, "video_operation_unsupported")
		return
	}
	price, err := ratio.GetModelPrice(actual, public)
	if err != nil {
		jobError(c, 422, "model_price_not_found")
		return
	}
	discount := ratio.GetGroupDiscount(m.Group, actual, public)
	estimate, err := handler.MeteredQuota(price, discount, []schema.UsageMetric{{Metric: "video.generated_duration", Quantity: int64(r.Duration), Scale: 1, Unit: "second"}})
	if err != nil {
		jobError(c, 422, "invalid_price")
		return
	}
	if estimate > 0 && price.BillingType != model.BillingTypePerRequest {
		estimate += config.PreConsumedQuota
	}
	publicBody, _ := json.Marshal(r)
	hash := sha256.Sum256(publicBody)
	r.Model = actual
	body, _ := json.Marshal(r)
	snapshot, _ := json.Marshal(inferencePriceSnapshot{Price: *price, Discount: discount})
	j := model.InferenceJob{ID: "job_" + random.GetUUID(), UserID: m.UserId, TokenID: m.TokenId, PlanID: m.PlanId, ChannelID: m.ChannelId, IdempotencyKey: key, RequestHash: hex.EncodeToString(hash[:]), ModelName: public, UpstreamModel: actual, ProviderID: m.ChannelID, BaseURL: m.BaseURL, Operation: r.Operation, RequestJSON: string(body), PriceSnapshot: string(snapshot), ReservedQuota: estimate, Status: "reserving", AssetStatus: "pending", SettlementStatus: "pending"}
	saved, created, err := model.CreateInferenceJob(c.Request.Context(), j)
	if err != nil {
		jobError(c, 409, "idempotency_conflict")
		return
	}
	if !created {
		c.JSON(202, saved)
		return
	}
	if err = reserveMediaJob(c.Request.Context(), saved); err != nil {
		status, code, terminal := mediaReservationFailure(err)
		updates := map[string]any{"error_code": code, "updated_at": time.Now().Unix()}
		if terminal {
			updates["status"] = "failed"
			updates["asset_status"] = "unavailable"
			updates["settlement_status"] = "not_reserved"
		}
		model.DB.Model(saved).Updates(updates)
		jobError(c, status, code)
		return
	}
	model.DB.Model(saved).Updates(map[string]any{"status": "queued", "updated_at": time.Now().Unix()})
	saved.Status = "queued"
	c.JSON(202, saved)
}

func mediaReservationFailure(err error) (int, string, bool) {
	switch err.Error() {
	case "insufficient user quota", "insufficient token quota", "insufficient plan quota":
		return 403, "insufficient_quota", true
	case "plan token window exhausted", "plan request window exhausted":
		return 429, "plan_limit_exceeded", true
	case "model not covered by plan":
		return 403, "plan_model_not_allowed", true
	case "invalid inference reservation", "reservation key reused with different intent":
		return 409, "reservation_conflict", true
	default:
		return 503, "reservation_unavailable", false
	}
}

func reserveMediaJob(ctx context.Context, j *model.InferenceJob) error {
	return model.ReserveInference(ctx, model.InferenceReservation{Key: "job:" + j.ID, UserID: j.UserID, TokenID: j.TokenID, PlanID: j.PlanID, ChannelID: j.ChannelID, ModelName: j.ModelName, Quota: j.ReservedQuota, RequestHash: j.RequestHash})
}

// GetMediaJob returns only jobs owned by both the authenticated account and creating token.
// 版本: v0.0.28
// 日期: 2026-11-23
func GetMediaJob(c *gin.Context) {
	j, err := model.GetInferenceJob(c.Request.Context(), c.Param("id"), c.GetInt(ctxkey.Id), c.GetInt(ctxkey.TokenId))
	if err != nil {
		jobError(c, 404, "job_not_found")
		return
	}
	c.JSON(200, j)
}

// CancelMediaJob marks a cancellation intent; quota is released only after cancellation is proven.
// 版本: v0.0.28
// 日期: 2026-11-23
func CancelMediaJob(c *gin.Context) {
	j, err := model.GetInferenceJob(c.Request.Context(), c.Param("id"), c.GetInt(ctxkey.Id), c.GetInt(ctxkey.TokenId))
	if err != nil {
		jobError(c, 404, "job_not_found")
		return
	}
	if j.Status == "succeeded" || j.Status == "failed" || j.Status == "canceled" || j.Status == "unknown" {
		jobError(c, 409, "job_cannot_cancel")
		return
	}
	result := model.DB.Model(&model.InferenceJob{}).Where("id = ? AND status IN ?", j.ID, []string{"reserving", "queued", "submitting", "submitted", "running"}).Updates(map[string]any{"cancel_requested": true, "next_poll_at": time.Now().Unix(), "updated_at": time.Now().Unix()})
	if result.Error != nil {
		jobError(c, 503, "job_storage_unavailable")
		return
	}
	if result.RowsAffected != 1 {
		jobError(c, 409, "job_cannot_cancel")
		return
	}
	j.CancelRequested = true
	c.JSON(202, j)
}

func mediaAssetDirectory() string {
	dir := os.Getenv("INFERENCE_ASSET_DIR")
	if dir == "" {
		dir = filepath.Join(*common.DataDir, "media-assets")
	}
	return dir
}

// GetMediaAsset streams server-owned unexpired media without disclosing provider URLs or filesystem paths.
// 版本: v0.0.28
// 日期: 2026-11-23
func GetMediaAsset(c *gin.Context) {
	var asset model.InferenceAsset
	if err := model.DB.Where("id = ? AND user_id = ? AND token_id = ? AND expires_at > ?", c.Param("id"), c.GetInt(ctxkey.Id), c.GetInt(ctxkey.TokenId), time.Now().Unix()).First(&asset).Error; err != nil {
		jobError(c, 404, "asset_not_found")
		return
	}
	if filepath.Base(asset.StorageKey) != asset.StorageKey {
		jobError(c, 404, "asset_not_found")
		return
	}
	f, err := os.Open(filepath.Join(mediaAssetDirectory(), asset.StorageKey))
	if err != nil {
		jobError(c, 404, "asset_unavailable")
		return
	}
	defer f.Close()
	info, err := f.Stat()
	if err != nil {
		jobError(c, 404, "asset_unavailable")
		return
	}
	c.Header("Content-Type", asset.ContentType)
	c.Header("Cache-Control", "private, no-store")
	c.Header("X-Content-Type-Options", "nosniff")
	c.Header("Content-Disposition", `attachment; filename="`+asset.ID+`.mp4"`)
	http.ServeContent(c.Writer, c.Request, asset.ID, info.ModTime(), f)
}

func jobError(c *gin.Context, status int, code string) {
	c.JSON(status, gin.H{"error": gin.H{"code": code, "type": "inference_error", "message": strings.ReplaceAll(code, "_", " ")}})
}

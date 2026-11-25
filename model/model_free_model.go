// model_free_model.go 免费模型清单
// Catalogue of free-tier models, per provider.
// 版本: v0.0.28
// 日期: 2026-11-25
// 作者: MCode
//
// 「免费」在本系统里是显式标记，不是推导出来的：
// model_price 的 0 价既可能表示真的免费，也可能只是「该模型没配价、
// 走 * 兜底价恰好是 0」，因此不能靠价格反推免费与否。
// 另外要区分三种不同的「免费」：永久免费模型、限时的免费额度、
// 以及套餐窗口额度——它们的过期与重置规则完全不同。
package model

import (
	"errors"
	"strings"
	"time"

	"gorm.io/gorm/clause"
)

// ErrFreeModelInvalid 表示免费模型记录缺少必要字段。
// ErrFreeModelInvalid means the free model record misses required fields.
var ErrFreeModelInvalid = errors.New("provider_id 与 model_name 不能为空")

// 免费模型类型常量。
// Free model type constants.
const (
	// FreeTypePermanent 永久免费模型，不限量。
	FreeTypePermanent = "permanent"
	// FreeTypeQuota 限时免费额度，有过期时间。
	FreeTypeQuota = "quota"
	// FreeTypePlan 套餐窗口额度（5 小时 / 日 / 周 / 月滚动窗口）。
	FreeTypePlan = "plan"
	// FreeTypeSuffix 按命名后缀识别，如 OpenRouter 的 ":free"。
	FreeTypeSuffix = "suffix"
)

// ModelFreeModel 记录某个供应商下的一个免费模型。
// ModelFreeModel records one free-tier model of a provider.
type ModelFreeModel struct {
	Id uint `json:"id" gorm:"primaryKey;autoIncrement"`
	// ProviderID 对应 relay/registry 的 ChannelMeta.ID。
	// ProviderID matches relay/registry ChannelMeta.ID.
	ProviderID string `json:"provider_id" gorm:"size:64;uniqueIndex:idx_provider_free_model;not null"`
	// ModelName 上游模型名。
	// Upstream model name.
	ModelName string `json:"model_name" gorm:"size:255;uniqueIndex:idx_provider_free_model;not null"`
	// FreeType 见 FreeType* 常量。
	// FreeType, see the FreeType* constants.
	FreeType string `json:"free_type" gorm:"size:32;default:'permanent'"`
	// Note 免费说明，例如「新人 100 万 token，90 天有效」。
	// Free tier note, e.g. "1M free tokens for new accounts, valid 90 days".
	Note string `json:"note" gorm:"type:text"`
	// ValidUntil 免费额度过期时间（Unix 秒），0 表示不过期。
	// Free tier expiry (Unix seconds); 0 means no expiry.
	ValidUntil int64 `json:"valid_until" gorm:"bigint;default:0"`
	// Enabled 是否启用。
	// Whether the entry is active.
	Enabled bool `json:"enabled" gorm:"default:true;not null"`
	// ZeroPrice 网关侧是否按 0 价计费（上游 0 元则用户也不付费）。
	// Whether the gateway bills this model at zero price.
	ZeroPrice bool `json:"zero_price" gorm:"default:false;not null"`
	// UpdatedAt 最后维护时间（Unix 秒）。
	// Last update time (Unix seconds).
	UpdatedAt int64 `json:"updated_at" gorm:"bigint;default:0"`
}

// UpsertModelFreeModel 按 (provider_id, model_name) 写入或更新免费模型记录。
// UpsertModelFreeModel writes or updates a free model record.
func UpsertModelFreeModel(item *ModelFreeModel) error {
	if item == nil || item.ProviderID == "" || item.ModelName == "" {
		return ErrFreeModelInvalid
	}
	item.UpdatedAt = time.Now().Unix()
	return DB.Clauses(clause.OnConflict{
		Columns:   []clause.Column{{Name: "provider_id"}, {Name: "model_name"}},
		DoUpdates: clause.AssignmentColumns([]string{"free_type", "note", "valid_until", "enabled", "zero_price", "updated_at"}),
	}).Create(item).Error
}

// GetModelFreeModels 返回免费模型清单；providerID 非空时只返回该供应商的。
// GetModelFreeModels returns the free model catalogue, optionally filtered by provider.
func GetModelFreeModels(providerID string) ([]*ModelFreeModel, error) {
	var out []*ModelFreeModel
	query := DB.Order("provider_id asc, model_name asc")
	if providerID != "" {
		query = query.Where("provider_id = ?", providerID)
	}
	if err := query.Find(&out).Error; err != nil {
		return nil, err
	}
	return out, nil
}

// DeleteModelFreeModel 删除一条免费模型记录。
// DeleteModelFreeModel removes one free model record.
func DeleteModelFreeModel(providerID, modelName string) error {
	return DB.Where("provider_id = ? AND model_name = ?", providerID, modelName).Delete(&ModelFreeModel{}).Error
}

// DetectFreeModelSuffix 按模型名后缀识别免费模型。
// Detects a free model by its name suffix.
//
// 覆盖三种写法：
//   - ":free"   OpenRouter 免费变体，如 "cohere/north-mini-code:free"
//   - "/free"   OpenRouter 自由路由别名，如 "openrouter/free"
//   - "-free"   部分中转站的免费模型，如 "vendor/model-free"
//
// 识别结果只是「疑似免费」，最终仍需管理员确认后写入清单。
func DetectFreeModelSuffix(modelName string) (bool, string) {
	lower := strings.ToLower(strings.TrimSpace(modelName))
	if lower == "" {
		return false, ""
	}
	if strings.HasSuffix(lower, ":free") {
		return true, ":free"
	}
	if strings.HasSuffix(lower, "/free") {
		return true, "/free"
	}
	if strings.HasSuffix(lower, "-free") {
		return true, "-free"
	}
	if strings.HasSuffix(lower, "_free") {
		return true, "_free"
	}
	return false, ""
}

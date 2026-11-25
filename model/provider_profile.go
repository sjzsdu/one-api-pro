// provider_profile.go 供应商策展信息
// Curated provider profile data (summary, signup links, free policy).
// 版本: v0.0.28
// 日期: 2026-11-25
// 作者: MCode
//
// 渠道元信息（ID / 名称 / 默认 BaseURL）来自 relay/registry，是权威且自动的；
// 但「怎么注册」「有没有免费额度」「注意事项」属于上游厂商信息，
// 系统无从推断，需要管理员策展维护在本表。
package model

import (
	"errors"
	"time"

	"gorm.io/gorm/clause"
)

// ErrProviderProfileInvalid 表示策展信息缺少 provider_id。
// ErrProviderProfileInvalid means the profile has no provider ID.
var ErrProviderProfileInvalid = errors.New("provider_id 不能为空")

// ProviderProfile 保存供应商的人工策展信息。
// ProviderProfile holds administrator-curated information about a provider.
type ProviderProfile struct {
	Id uint `json:"id" gorm:"primaryKey;autoIncrement"`
	// ProviderID 对应 relay/registry 的 ChannelMeta.ID（如 alibailian、openrouter）。
	// ProviderID matches relay/registry ChannelMeta.ID (e.g. alibailian, openrouter).
	ProviderID string `json:"provider_id" gorm:"size:64;uniqueIndex;not null"`
	// Title 展示名，留空时回落到 registry 的 Name。
	// Display title; falls back to the registry Name when empty.
	Title string `json:"title" gorm:"size:128;default:''"`
	// Summary 一句话简介。
	// One-line summary.
	Summary string `json:"summary" gorm:"type:text"`
	// Category 分类标签：国产 / 海外 / 中转 / 本地。
	// Category label: 国产 (domestic) / 海外 (overseas) / 中转 (reseller) / 本地 (local).
	Category string `json:"category" gorm:"size:32;default:''"`
	// SignupURL 申请 / 注册地址。
	// Signup URL.
	SignupURL string `json:"signup_url" gorm:"size:512;default:''"`
	// ConsoleURL 控制台地址。
	// Console URL.
	ConsoleURL string `json:"console_url" gorm:"size:512;default:''"`
	// DocURL 官方文档地址。
	// Official documentation URL.
	DocURL string `json:"doc_url" gorm:"size:512;default:''"`
	// FreePolicy 免费额度政策说明。
	// Free tier policy description.
	FreePolicy string `json:"free_policy" gorm:"type:text"`
	// Notes 接入注意事项。
	// Integration notes.
	Notes string `json:"notes" gorm:"type:text"`
	// UpdatedAt 最后维护时间（Unix 秒）。
	// Last update time (Unix seconds).
	UpdatedAt int64 `json:"updated_at" gorm:"bigint;default:0"`
}

// UpsertProviderProfile 按 provider_id 写入或更新策展信息。
// UpsertProviderProfile writes or updates a profile by provider ID.
func UpsertProviderProfile(item *ProviderProfile) error {
	if item == nil || item.ProviderID == "" {
		return ErrProviderProfileInvalid
	}
	item.UpdatedAt = time.Now().Unix()
	return DB.Clauses(clause.OnConflict{
		Columns:   []clause.Column{{Name: "provider_id"}},
		DoUpdates: clause.AssignmentColumns([]string{"title", "summary", "category", "signup_url", "console_url", "doc_url", "free_policy", "notes", "updated_at"}),
	}).Create(item).Error
}

// GetProviderProfiles 返回全部策展信息。
// GetProviderProfiles returns every curated profile.
func GetProviderProfiles() ([]*ProviderProfile, error) {
	var out []*ProviderProfile
	if err := DB.Order("provider_id asc").Find(&out).Error; err != nil {
		return nil, err
	}
	return out, nil
}

// DeleteProviderProfile 按 provider_id 删除策展信息。
// DeleteProviderProfile removes a profile by provider ID.
func DeleteProviderProfile(providerID string) error {
	return DB.Where("provider_id = ?", providerID).Delete(&ProviderProfile{}).Error
}

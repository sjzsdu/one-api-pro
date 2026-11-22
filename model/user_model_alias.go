// user_model_alias.go 用户独立的业务模型别名
// Persists account-local aliases without changing channel mappings.
// 版本: v0.0.28
// 日期: 2026-11-22
// 作者: Codex
package model

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"gorm.io/gorm"
	"regexp"
	"strings"
)

// UserModelAlias maps one account-local name to a concrete gateway model.
// 版本: v0.0.28
// 日期: 2026-11-22
type UserModelAlias struct {
	UserId            int                    `json:"-" gorm:"primaryKey;autoIncrement:false"`
	Alias             string                 `json:"alias" gorm:"primaryKey;size:64"`
	TargetModel       string                 `json:"target_model" gorm:"size:255;not null"`
	TargetsJSON       string                 `json:"-" gorm:"type:text"`
	Targets           []UserModelAliasTarget `json:"targets" gorm:"-"`
	FailoverEnabled   *bool                  `json:"failover_enabled" gorm:"default:true"`
	SessionTTLSeconds int                    `json:"session_ttl_seconds" gorm:"default:3600"`
	Source            string                 `json:"source,omitempty" gorm:"-"`
}

var userAliasName = regexp.MustCompile(`^[a-zA-Z][a-zA-Z0-9._-]{0,63}$`)

// GetUserModelAliases returns only aliases owned by the authenticated account.
// 版本: v0.0.28
// 日期: 2026-11-22
func GetUserModelAliases(userId int) ([]UserModelAlias, error) {
	return cachedUserModelAliases(userId)
}

// ResolveUserModelAlias resolves exactly one hop; missing aliases preserve ordinary requests.
// 版本: v0.0.28
// 日期: 2026-11-22
func ResolveUserModelAlias(userId int, name string) (string, error) {
	items, err := GetEffectiveUserModelAliases(userId)
	if err != nil {
		return "", err
	}
	for _, item := range items {
		if item.Alias == strings.ToLower(name) {
			return item.TargetModel, nil
		}
	}
	return name, nil
}

// SaveUserModelAliases validates and atomically replaces one user's aliases.
// 版本: v0.0.28
// 日期: 2026-11-22
func SaveUserModelAliases(ctx context.Context, userId int, items []UserModelAlias) error {
	if len(items) > 50 {
		return errors.New("最多配置 50 个模型别名")
	}
	var available []string
	var err error
	if userId == 0 {
		available, _, err = GetGlobalAliasTargets(ctx)
	} else {
		var group string
		group, err = GetUserGroup(userId)
		if err == nil {
			available, err = GetGroupModels(ctx, group)
		}
	}
	if err != nil {
		return err
	}
	concrete := map[string]bool{}
	for _, name := range available {
		concrete[name] = true
	}
	concreteNames := map[string]bool{}
	for name := range concrete {
		concreteNames[strings.ToLower(name)] = true
	}
	aliases := map[string]bool{}
	for i := range items {
		item := &items[i]
		item.Alias = strings.ToLower(strings.TrimSpace(item.Alias))
		if err := normalizeAliasPool(item); err != nil {
			return err
		}
		item.TargetModel = strings.TrimSpace(item.TargetModel)
		item.UserId = userId
		if !userAliasName.MatchString(item.Alias) || strings.EqualFold(item.Alias, "auto") || item.Alias == "*" {
			return errors.New("别名须以英文字母开头，最多 64 位，仅含字母、数字、点、横线、下划线，且不能为 auto")
		}
		if aliases[item.Alias] || concreteNames[item.Alias] {
			return fmt.Errorf("别名重复或与现有模型重名：%s", item.Alias)
		}
		aliases[item.Alias] = true
	}
	for _, item := range items {
		for _, target := range item.Targets {
			if aliases[strings.ToLower(target.Model)] || strings.EqualFold(target.Model, "auto") || target.Model == "*" || !concrete[target.Model] {
				return fmt.Errorf("目标须为有权使用的具体模型，不能为其他别名：%s", target.Model)
			}
		}
	}
	lock := userAliasLock(userId)
	lock.Lock()
	defer lock.Unlock()
	err = DB.Transaction(func(tx *gorm.DB) error {
		if err := tx.Where("user_id = ?", userId).Delete(&UserModelAlias{}).Error; err != nil {
			return err
		}
		if len(items) > 0 {
			return tx.Create(&items).Error
		}
		return nil
	})
	if err != nil {
		return err
	}
	return invalidateUserModelAliases(userId)
}

// GetUserAliasTargets returns routable models and optional catalog capabilities.
// 版本: v0.0.28
// 日期: 2026-11-22
func GetUserAliasTargets(ctx context.Context, userId int) ([]string, []ModelCatalog, error) {
	group, err := GetUserGroup(userId)
	if err != nil {
		return nil, nil, err
	}
	names, err := GetGroupModels(ctx, group)
	if err != nil {
		return nil, nil, err
	}
	metadata := []ModelCatalog{}
	if len(names) > 0 {
		if err = DB.Where("model_name IN ?", names).Order("updated_at DESC").Find(&metadata).Error; err != nil {
			return nil, nil, err
		}
	}
	return names, metadata, nil
}

// ErrUserModelAliasTargetUnavailable identifies a saved alias whose target is no longer allowed.
// 版本: v0.0.28
// 日期: 2026-11-22
var ErrUserModelAliasTargetUnavailable = errors.New("别名目标模型已不可用，请修改模型别名配置")

// ResolveAvailableUserModelAlias resolves only aliases still allowed by the account's current group.
// 版本: v0.0.28
// 日期: 2026-11-22
func ResolveAvailableUserModelAlias(ctx context.Context, userId int, name string) (string, bool, error) {
	items, err := GetEffectiveUserModelAliases(userId)
	if err != nil {
		return "", false, err
	}
	var candidate *UserModelAlias
	for i := range items {
		if items[i].Alias == strings.ToLower(name) {
			candidate = &items[i]
			break
		}
	}
	if candidate == nil {
		return name, false, nil
	}
	group, err := CacheGetUserGroup(userId)
	if err != nil {
		return "", false, err
	}
	names, err := CacheGetGroupModels(ctx, group)
	if err != nil {
		return "", false, err
	}
	for _, concrete := range names {
		if concrete == name {
			return name, false, nil
		}
	}
	for _, target := range candidate.Targets {
		for _, concrete := range names {
			if concrete == target.Model {
				return target.Model, true, nil
			}
		}
	}
	return "", true, ErrUserModelAliasTargetUnavailable
}

// GetUserModelAliasesWithGroupModels loads discovery inputs without querying groups for accounts with no aliases.
// 版本: v0.0.28
// 日期: 2026-11-22
func GetUserModelAliasesWithGroupModels(ctx context.Context, userId int) ([]UserModelAlias, map[string]bool, error) {
	items, err := GetEffectiveUserModelAliases(userId)
	if err != nil || len(items) == 0 {
		return items, nil, err
	}
	group, err := CacheGetUserGroup(userId)
	if err != nil {
		return nil, nil, err
	}
	names, err := CacheGetGroupModels(ctx, group)
	if err != nil {
		return nil, nil, err
	}
	available := make(map[string]bool, len(names))
	for _, name := range names {
		available[name] = true
	}
	return items, available, nil
}

// UserModelAliasTarget is one weighted concrete model in an alias pool.
// 版本: v0.0.28
// 日期: 2026-11-22
type UserModelAliasTarget struct {
	Model  string `json:"model"`
	Weight int    `json:"weight"`
}

func normalizeAliasPool(item *UserModelAlias) error {
	if len(item.Targets) == 0 && item.TargetsJSON != "" {
		if err := json.Unmarshal([]byte(item.TargetsJSON), &item.Targets); err != nil {
			return err
		}
	}
	if len(item.Targets) == 0 && item.TargetModel != "" {
		item.Targets = []UserModelAliasTarget{{Model: strings.TrimSpace(item.TargetModel), Weight: 1}}
	}
	if len(item.Targets) == 0 || len(item.Targets) > 8 {
		return errors.New("每个别名需要 1–8 个候选模型")
	}
	seen := map[string]bool{}
	for i := range item.Targets {
		t := &item.Targets[i]
		t.Model = strings.TrimSpace(t.Model)
		if t.Weight == 0 {
			t.Weight = 1
		}
		if t.Model == "" || seen[t.Model] || t.Weight < 1 || t.Weight > 1000 {
			return errors.New("候选模型须不重复，权重须为 1–1000")
		}
		seen[t.Model] = true
	}
	if item.SessionTTLSeconds == 0 {
		item.SessionTTLSeconds = 3600
	}
	if item.SessionTTLSeconds < 60 || item.SessionTTLSeconds > 86400 {
		return errors.New("会话固定时间须为 60–86400 秒")
	}
	if item.FailoverEnabled == nil {
		enabled := true
		item.FailoverEnabled = &enabled
	}
	item.TargetModel = item.Targets[0].Model
	raw, err := json.Marshal(item.Targets)
	item.TargetsJSON = string(raw)
	return err
}

// GetEffectiveUserModelAliases merges personal settings over explicitly published global aliases.
// 版本: v0.0.28
// 日期: 2026-11-22
func GetEffectiveUserModelAliases(userId int) ([]UserModelAlias, error) {
	personal, err := GetUserModelAliases(userId)
	if err != nil {
		return nil, err
	}
	if userId == 0 {
		return personal, nil
	}
	global, err := GetUserModelAliases(0)
	if err != nil {
		return nil, err
	}
	result := []UserModelAlias{}
	seen := map[string]bool{}
	for _, item := range personal {
		item.Source = "personal"
		result = append(result, item)
		seen[item.Alias] = true
	}
	for _, item := range global {
		if !seen[item.Alias] {
			item.Source = "global"
			result = append(result, item)
		}
	}
	return result, nil
}

// GetGlobalAliasTargets lists enabled concrete models across all groups for root configuration.
// 版本: v0.0.28
// 日期: 2026-11-22
func GetGlobalAliasTargets(ctx context.Context) ([]string, []ModelCatalog, error) {
	var channels []Channel
	if err := DB.Where("status = ?", ChannelStatusEnabled).Find(&channels).Error; err != nil {
		return nil, nil, err
	}
	names := []string{}
	seen := map[string]bool{}
	for _, ch := range channels {
		for _, name := range strings.Split(ch.Models, ",") {
			name = strings.TrimSpace(name)
			if name != "" && name != "*" && name != "auto" && !seen[name] {
				names = append(names, name)
				seen[name] = true
			}
		}
	}
	metadata := []ModelCatalog{}
	if len(names) > 0 && DB.Migrator().HasTable(&ModelCatalog{}) {
		if err := DB.Where("model_name IN ?", names).Find(&metadata).Error; err != nil {
			return nil, nil, err
		}
	}
	return names, metadata, nil
}

// ErrAliasPoolCooling is transient and should be surfaced as HTTP 503.
// 版本: v0.0.28
// 日期: 2026-11-22
var ErrAliasPoolCooling = errors.New("模型池所有候选模型正在故障冷却，请稍后重试")

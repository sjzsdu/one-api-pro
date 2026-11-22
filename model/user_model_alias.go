// user_model_alias.go 用户独立的业务模型别名
// Persists account-local aliases without changing channel mappings.
// 版本: v0.0.28
// 日期: 2026-11-21
// 作者: Codex
package model

import (
	"context"
	"errors"
	"fmt"
	"gorm.io/gorm"
	"regexp"
	"strings"
)

// UserModelAlias maps one account-local name to a concrete gateway model.
// 版本: v0.0.28
// 日期: 2026-11-21
type UserModelAlias struct {
	UserId      int    `json:"-" gorm:"primaryKey;autoIncrement:false"`
	Alias       string `json:"alias" gorm:"primaryKey;size:64"`
	TargetModel string `json:"target_model" gorm:"size:255;not null"`
}

var userAliasName = regexp.MustCompile(`^[a-zA-Z][a-zA-Z0-9._-]{0,63}$`)

// GetUserModelAliases returns only aliases owned by the authenticated account.
// 版本: v0.0.28
// 日期: 2026-11-21
func GetUserModelAliases(userId int) ([]UserModelAlias, error) {
	return cachedUserModelAliases(userId)
}

// ResolveUserModelAlias resolves exactly one hop; missing aliases preserve ordinary requests.
// 版本: v0.0.28
// 日期: 2026-11-21
func ResolveUserModelAlias(userId int, name string) (string, error) {
	items, err := GetUserModelAliases(userId)
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
// 日期: 2026-11-21
func SaveUserModelAliases(ctx context.Context, userId int, items []UserModelAlias) error {
	if len(items) > 50 {
		return errors.New("最多配置 50 个模型别名")
	}
	group, err := GetUserGroup(userId)
	if err != nil {
		return err
	}
	available, err := GetGroupModels(ctx, group)
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
		if aliases[strings.ToLower(item.TargetModel)] || strings.EqualFold(item.TargetModel, "auto") || item.TargetModel == "*" || !concrete[item.TargetModel] {
			return fmt.Errorf("目标须为有权使用的具体模型，不能为其他别名：%s", item.TargetModel)
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
// 日期: 2026-11-21
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
// 日期: 2026-11-21
var ErrUserModelAliasTargetUnavailable = errors.New("别名目标模型已不可用，请修改模型别名配置")

// ResolveAvailableUserModelAlias resolves only aliases still allowed by the account's current group.
// 版本: v0.0.28
// 日期: 2026-11-21
func ResolveAvailableUserModelAlias(ctx context.Context, userId int, name string) (string, bool, error) {
	items, err := GetUserModelAliases(userId)
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
	for _, concrete := range names {
		if concrete == candidate.TargetModel {
			return candidate.TargetModel, true, nil
		}
	}
	return "", true, ErrUserModelAliasTargetUnavailable
}

// GetUserModelAliasesWithGroupModels loads discovery inputs without querying groups for accounts with no aliases.
// 版本: v0.0.28
// 日期: 2026-11-21
func GetUserModelAliasesWithGroupModels(ctx context.Context, userId int) ([]UserModelAlias, map[string]bool, error) {
	items, err := GetUserModelAliases(userId)
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

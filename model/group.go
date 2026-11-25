package model

import (
	"sort"
	"strings"

	"github.com/modelbus/one-api-pro/common"
)

// groupColumnRef 返回 group 列在 SQL 中的引用写法。
// group 是 MySQL / PostgreSQL 的保留字，作为裸列名拼接会触发语法错误，
// 因此统一走这个 helper（与 GetGroupModels 的处理保持一致）。
//
// 引用形式由 common.QuoteIdentifier 统一决定：PostgreSQL 用双引号，
// MySQL / SQLite 用反引号（PostgreSQL 不接受反引号）。
// 版本: v0.0.28
// 日期: 2026-11-25
func groupColumnRef() string {
	return common.QuoteIdentifier("group")
}

// GetAllGroupNames 返回系统中「真实存在」的全部分组名，去重后按字典序排序。
// 数据来源：分组价格配置 + 渠道分组 + 用户分组。
//
// 为什么不能只用 GetGroupNames()（分组价格表）：
// 分组价格表只包含「配置过模型倍率」的分组。一个分组如果只被渠道或用户使用、
// 却没有单独配价格，就永远不会出现在 /api/group/ 的返回里。管理端编辑渠道时
// 的下拉正是消费这个接口，于是会出现「这个分组明明存在，却选不到/选了不生效」。
func GetAllGroupNames() []string {
	seen := make(map[string]struct{})
	add := func(values ...string) {
		for _, value := range values {
			value = strings.TrimSpace(value)
			if value != "" {
				seen[value] = struct{}{}
			}
		}
	}

	// 1. 分组价格表
	add(GetGroupNames()...)

	// 2. 渠道分组（一个渠道可属于多个分组，逗号分隔）
	var channelGroups []string
	if err := DB.Model(&Channel{}).
		Distinct(groupColumnRef()).
		Pluck(groupColumnRef(), &channelGroups).Error; err == nil {
		for _, raw := range channelGroups {
			add(strings.Split(raw, ",")...)
		}
	}

	// 3. 用户分组（排除已删除用户；用户分组同样是逗号分隔）
	var userGroups []string
	if err := DB.Model(&User{}).
		Where("status != ?", UserStatusDeleted).
		Distinct(groupColumnRef()).
		Pluck(groupColumnRef(), &userGroups).Error; err == nil {
		for _, raw := range userGroups {
			add(strings.Split(raw, ",")...)
		}
	}

	names := make([]string, 0, len(seen))
	for name := range seen {
		names = append(names, name)
	}
	sort.Strings(names)
	return names
}

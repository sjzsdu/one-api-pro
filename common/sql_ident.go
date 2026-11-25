// sql_ident.go 跨方言的 SQL 标识符引用
// Dialect-aware SQL identifier quoting for reserved column names.
// 版本: v0.0.28
// 日期: 2026-11-25
// 作者: MCode
package common

import "strings"

// QuoteIdentifier 按当前数据库方言返回列名的安全引用形式。
// Returns a safely quoted identifier for the active database dialect.
//
// group / key 是 SQL 保留字：MySQL 与 SQLite 接受反引号，PostgreSQL 只认双引号，
// 而且 PostgreSQL 完全不接受反引号——手写 SQL 里写成 `group` 会让整条语句语法错误，
// GORM 只会拿到一个 error，候选集恒为空。
//
// 线上事故（2026-11-24）：PostgreSQL 部署 + MEMORY_CACHE_ENABLED 未开启时，
// 渠道候选查询整条报 syntax error → 所有模型一律 503「无可用渠道」，
// 而 /v1/models 因为走的是另一条不引用 group 列的查询所以照常返回清单，
// 排查时极易误判为"分组没配渠道"。
//
// 约定：所有手写 SQL 片段中的列名都必须经过本函数，不要直接拼接字符串。
func QuoteIdentifier(name string) string {
	if name == "" {
		return ""
	}
	if UsingPostgreSQL {
		return `"` + strings.ReplaceAll(name, `"`, `""`) + `"`
	}
	return "`" + strings.ReplaceAll(name, "`", "``") + "`"
}

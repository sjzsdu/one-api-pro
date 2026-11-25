// sql_ident_guard_test.go 防回归：禁止在原始 SQL 里直写反引号列名
// Guard test: no raw backtick-quoted identifiers in hand-written SQL.
// 版本: v0.0.28
// 日期: 2026-11-25
// 作者: MCode
package model

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// quotedIdentifiers 是必须通过 common.QuoteIdentifier 生成的保留字列名。
// Reserved column names that must go through common.QuoteIdentifier.
var quotedIdentifiers = []string{"`group`", "`key`"}

// sqlIdentGuardAllowed 是允许直接出现反引号列名的文件。
// Files allowed to contain backtick-quoted identifiers literally.
var sqlIdentGuardAllowed = map[string]bool{
	// 唯一允许手写反引号的地方：引用形式本身在这里产生。
	filepath.Join("common", "sql_ident.go"): true,
}

// TestNoRawBacktickIdentifiersInSQL 扫描全仓非测试 Go 文件，
// 发现手写 SQL 片段里的 `group` / `key` 就失败。
//
// 背景：PostgreSQL 完全不接受反引号，`group` / `key` 又是保留字，
// 写成 `group` 会让整条语句 syntax error —— 线上曾因此让所有模型 503「无可用渠道」，
// 而 /v1/models 走另一条查询照常返回清单，极难排查。
// CI 只有 SQLite，单测无法覆盖 PostgreSQL，故在此用源码扫描兜底。
func TestNoRawBacktickIdentifiersInSQL(t *testing.T) {
	root := ".."
	var offenders []string

	err := filepath.Walk(root, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		if info.IsDir() {
			switch info.Name() {
			case ".git", "node_modules", "web", "dist", "tmp_pg_repro":
				return filepath.SkipDir
			}
			return nil
		}
		if !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
			return nil
		}
		rel, relErr := filepath.Rel(root, path)
		if relErr != nil {
			rel = path
		}
		if sqlIdentGuardAllowed[rel] {
			return nil
		}
		data, readErr := os.ReadFile(path)
		if readErr != nil {
			return readErr
		}
		code := stripGoComments(string(data))
		for _, ident := range quotedIdentifiers {
			if strings.Contains(code, ident) {
				offenders = append(offenders, rel+" 出现 "+ident)
			}
		}
		return nil
	})
	if err != nil {
		t.Fatalf("scan repository: %v", err)
	}
	if len(offenders) > 0 {
		t.Fatalf("原始 SQL 中禁止直写反引号列名，请改用 common.QuoteIdentifier:\n%s",
			strings.Join(offenders, "\n"))
	}
}

// stripGoComments 去掉注释内容，避免文档里的示例触发误报。
// Removes comments so documentation examples do not trip the guard.
func stripGoComments(src string) string {
	var out strings.Builder
	out.Grow(len(src))
	for i := 0; i < len(src); {
		switch {
		case strings.HasPrefix(src[i:], "//"):
			end := strings.IndexByte(src[i:], '\n')
			if end < 0 {
				i = len(src)
				continue
			}
			i += end
		case strings.HasPrefix(src[i:], "/*"):
			end := strings.Index(src[i+2:], "*/")
			if end < 0 {
				i = len(src)
				continue
			}
			i += 2 + end + 2
		case src[i] == '"' || src[i] == '`' || src[i] == '\'':
			quote := src[i]
			out.WriteByte(src[i])
			i++
			for i < len(src) {
				out.WriteByte(src[i])
				if src[i] == '\\' && i+1 < len(src) {
					i++
					out.WriteByte(src[i])
					i++
					continue
				}
				if src[i] == quote {
					i++
					break
				}
				i++
			}
		default:
			out.WriteByte(src[i])
			i++
		}
	}
	return out.String()
}

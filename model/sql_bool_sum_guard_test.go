// sql_bool_sum_guard_test.go 防回归：禁止在原始 SQL 里对布尔表达式做 SUM
// Guard test: no SUM() over boolean expressions in hand-written SQL.
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

// TestNoBooleanSumInSQL 扫描全仓非测试 Go 文件，
// 发现 SUM(<含比较运算符的表达式>) 就失败。
//
// 背景：MySQL / SQLite 允许 SUM(status = 1)（布尔当 0/1 求和），
// PostgreSQL 没有 sum(boolean) 函数，直接写会报
// "function sum(boolean) does not exist"（SQLSTATE 42883），
// 管理员仪表盘整页 500。CI 只有 SQLite，单测覆盖不到 PostgreSQL，
// 故在此用源码扫描兜底。
func TestNoBooleanSumInSQL(t *testing.T) {
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
		data, readErr := os.ReadFile(path)
		if readErr != nil {
			return readErr
		}
		code := stripGoComments(string(data))
		for _, expr := range findSumArguments(code) {
			if containsComparison(expr) {
				offenders = append(offenders, rel+" 出现 SUM("+expr+")")
			}
		}
		return nil
	})
	if err != nil {
		t.Fatalf("scan repository: %v", err)
	}
	if len(offenders) > 0 {
		t.Fatalf("原始 SQL 中禁止对布尔表达式做 SUM，请改用 SUM(CASE WHEN ... THEN 1 ELSE 0 END):\n%s",
			strings.Join(offenders, "\n"))
	}
}

// findSumArguments 提取所有 SUM(...) 的括号内内容。
// Extracts the parenthesised argument of every SUM(...) call.
func findSumArguments(code string) []string {
	var out []string
	lower := strings.ToLower(code)
	for idx := 0; idx < len(code); {
		pos := strings.Index(lower[idx:], "sum(")
		if pos < 0 {
			return out
		}
		abs := idx + pos
		// 排除 md5.Sum( / h.Sum( 这类 Go 方法调用。
		if abs > 0 && isIdentifierByte(code[abs-1]) {
			idx = abs + len("sum(")
			continue
		}
		start := abs + len("sum(")
		depth := 1
		end := start
	scan:
		for ; end < len(code); end++ {
			switch code[end] {
			case '(':
				depth++
			case ')':
				depth--
				if depth == 0 {
					break scan
				}
			}
		}
		if depth != 0 {
			return out
		}
		out = append(out, code[start:end])
		idx = end + 1
	}
	return out
}

// isIdentifierByte 判断字节是否属于标识符字符。
// Reports whether the byte can appear inside an identifier.
func isIdentifierByte(c byte) bool {
	return c == '_' || c == '.' ||
		(c >= '0' && c <= '9') ||
		(c >= 'a' && c <= 'z') ||
		(c >= 'A' && c <= 'Z')
}

// containsComparison 判断表达式里是否含比较运算符。
// Reports whether the expression contains a comparison operator.
func containsComparison(expr string) bool {
	trimmed := strings.TrimSpace(expr)
	// CASE WHEN 形式三方言通用，属于合法写法。
	if strings.HasPrefix(strings.ToUpper(trimmed), "CASE") {
		return false
	}
	for i := 0; i < len(expr); i++ {
		switch expr[i] {
		case '=', '!', '<', '>':
			return true
		}
	}
	return false
}

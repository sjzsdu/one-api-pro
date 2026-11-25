// sql_ident_test.go 方言感知标识符引用单测
// Unit tests for dialect-aware SQL identifier quoting.
// 版本: v0.0.28
// 日期: 2026-11-25
// 作者: MCode
package common

import "testing"

// TestQuoteIdentifierPerDialect 锁定两个方言的引用形式。
// Locks the quoting form for PostgreSQL and MySQL/SQLite.
func TestQuoteIdentifierPerDialect(t *testing.T) {
	old := UsingPostgreSQL
	t.Cleanup(func() { UsingPostgreSQL = old })

	UsingPostgreSQL = true
	if got := QuoteIdentifier("group"); got != `"group"` {
		t.Fatalf("postgres QuoteIdentifier(group) = %q, want %q", got, `"group"`)
	}
	if got := QuoteIdentifier("key"); got != `"key"` {
		t.Fatalf("postgres QuoteIdentifier(key) = %q, want %q", got, `"key"`)
	}

	UsingPostgreSQL = false
	if got := QuoteIdentifier("group"); got != "`group`" {
		t.Fatalf("mysql QuoteIdentifier(group) = %q, want %q", got, "`group`")
	}
	if got := QuoteIdentifier("key"); got != "`key`" {
		t.Fatalf("mysql QuoteIdentifier(key) = %q, want %q", got, "`key`")
	}
	if got := QuoteIdentifier(""); got != "" {
		t.Fatalf("QuoteIdentifier(\"\") = %q, want empty", got)
	}
}

// TestQuoteIdentifierEscapesEmbeddedQuote 保证列名里的引号被转义，不会拼出坏 SQL。
// Ensures embedded quotes are escaped instead of breaking the statement.
func TestQuoteIdentifierEscapesEmbeddedQuote(t *testing.T) {
	old := UsingPostgreSQL
	t.Cleanup(func() { UsingPostgreSQL = old })

	UsingPostgreSQL = true
	if got := QuoteIdentifier(`gr"oup`); got != `"gr""oup"` {
		t.Fatalf("postgres QuoteIdentifier(gr\"oup) = %q, want %q", got, `"gr""oup"`)
	}

	UsingPostgreSQL = false
	if got := QuoteIdentifier("gr`oup"); got != "`gr``oup`" {
		t.Fatalf("mysql QuoteIdentifier(gr`oup) = %q, want %q", got, "`gr``oup`")
	}
}

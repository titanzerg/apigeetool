package cmd

import (
	"net/url"
	"strings"
	"testing"
)

func TestQuoteTableName(t *testing.T) {
	got, err := quoteTableName(`public.table`)
	if err != nil {
		t.Fatalf("quoteTableName error: %v", err)
	}
	if got != `"public"."table"` {
		t.Fatalf("quoteTableName = %q, want \"public\".\"table\"", got)
	}
	if _, err := quoteTableName(`bad"part`); err == nil {
		t.Fatalf("expected error for invalid identifier")
	}
}

func TestSanitizeDSN(t *testing.T) {
	in := "postgres://user:pa%ss@host/db"
	got := sanitizeDSN(in)
	if strings.Contains(got, "%ss") {
		t.Fatalf("sanitizeDSN should escape stray percent, got %q", got)
	}
}

func TestEnforceSSLMode(t *testing.T) {
	q := url.Values{}
	enforceSSLMode(q)
	if mode := q.Get("sslmode"); mode != "require" {
		t.Fatalf("sslmode = %q, want require", mode)
	}
	q.Set("sslmode", "verify-full")
	enforceSSLMode(q)
	if mode := q.Get("sslmode"); mode != "verify-full" {
		t.Fatalf("sslmode should stay verify-full, got %q", mode)
	}
}

func TestBuildDatabaseURLEnforcesSSLMode(t *testing.T) {
	opts := dbConnOptions{URL: "postgres://user:pass@localhost/db?sslmode=disable"}
	got, err := buildDatabaseURL(opts)
	if err != nil {
		t.Fatalf("buildDatabaseURL error: %v", err)
	}
	if !strings.Contains(got, "sslmode=require") {
		t.Fatalf("expected enforced sslmode=require in %q", got)
	}
}

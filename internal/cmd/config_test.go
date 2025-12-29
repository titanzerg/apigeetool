package cmd

import (
	"testing"
)

const (
	testEnvOrg   = "env-org"
	testEnvToken = "env-token"
)

func TestResolveApigeeConfigPrefersFlags(t *testing.T) {
	t.Setenv("APIGEE_ORG", testEnvOrg)
	t.Setenv("APIGEE_TOKEN", testEnvToken)

	cfg := ResolveApigeeConfig("flag-org", "flag-token", "")
	if cfg.Org != "flag-org" {
		t.Fatalf("Org = %q, want flag-org", cfg.Org)
	}
	if cfg.Token != "flag-token" {
		t.Fatalf("Token = %q, want flag-token", cfg.Token)
	}
	if cfg.Host != "https://apigee.googleapis.com" {
		t.Fatalf("Host = %q, want default https://apigee.googleapis.com", cfg.Host)
	}
}

func TestResolveApigeeConfigFallsBackToEnv(t *testing.T) {
	t.Setenv("APIGEE_ORG", testEnvOrg)
	t.Setenv("APIGEE_TOKEN", testEnvToken)
	cfg := ResolveApigeeConfig("", "", "")
	if cfg.Org != testEnvOrg || cfg.Token != testEnvToken {
		t.Fatalf("expected env fallback, got org=%q token=%q", cfg.Org, cfg.Token)
	}
}

func TestRequireApigeeAuth(t *testing.T) {
	if err := RequireApigeeAuth(ApigeeConfig{Org: "o", Token: "t"}, "test"); err != nil {
		t.Fatalf("RequireApigeeAuth valid: %v", err)
	}
	if err := RequireApigeeAuth(ApigeeConfig{}, "test"); err == nil {
		t.Fatalf("RequireApigeeAuth should error on empty config")
	}
}

func TestFirstNonEmpty(t *testing.T) {
	if got := FirstNonEmpty("", "  ", "ok", "later"); got != "ok" {
		t.Fatalf("FirstNonEmpty returned %q, want ok", got)
	}
	if got := FirstNonEmpty(); got != "" {
		t.Fatalf("FirstNonEmpty empty values = %q, want \"\"", got)
	}
}

func TestDefaultString(t *testing.T) {
	if got := DefaultString(" value ", "fallback"); got != " value " {
		t.Fatalf("DefaultString should keep provided value, got %q", got)
	}
	if got := DefaultString("   ", "fallback"); got != "fallback" {
		t.Fatalf("DefaultString should return fallback, got %q", got)
	}
	if got := DefaultString("", "fallback"); got != "fallback" {
		t.Fatalf("DefaultString should return fallback on empty, got %q", got)
	}
}

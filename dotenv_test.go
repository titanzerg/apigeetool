package main

import (
	"os"
	"testing"
)

func TestParseDotEnvFile(t *testing.T) {
	tmp, err := os.CreateTemp("", "envtest")
	if err != nil {
		t.Fatalf("CreateTemp: %v", err)
	}
	defer os.Remove(tmp.Name())

	content := "FOO=bar\n#comment\nexport BAZ=qux\n"
	if err := os.WriteFile(tmp.Name(), []byte(content), 0o644); err != nil {
		t.Fatalf("write temp env: %v", err)
	}

	if err := parseDotEnvFile(tmp.Name()); err != nil {
		t.Fatalf("parseDotEnvFile: %v", err)
	}
	if got := os.Getenv("FOO"); got != "bar" {
		t.Fatalf("FOO=%q, want bar", got)
	}
	if got := os.Getenv("BAZ"); got != "qux" {
		t.Fatalf("BAZ=%q, want qux", got)
	}
}

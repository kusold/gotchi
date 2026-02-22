package main

import (
	"os"
	"path/filepath"
	"testing"
)

func TestRunInitParsesFlagsAfterAppName(t *testing.T) {
	tmp := t.TempDir()
	err := runInit([]string{"sample-app", "--output", tmp, "--module", "github.com/example/sample-app"})
	if err != nil {
		t.Fatalf("runInit returned error: %v", err)
	}

	goModPath := filepath.Join(tmp, "sample-app", "go.mod")
	if _, err := os.Stat(goModPath); err != nil {
		t.Fatalf("expected generated go.mod at %s: %v", goModPath, err)
	}
}

func TestRunInitParsesFlagsBeforeAppName(t *testing.T) {
	tmp := t.TempDir()
	err := runInit([]string{"--output", tmp, "--module", "github.com/example/another-app", "another-app"})
	if err != nil {
		t.Fatalf("runInit returned error: %v", err)
	}

	readmePath := filepath.Join(tmp, "another-app", "README.md")
	if _, err := os.Stat(readmePath); err != nil {
		t.Fatalf("expected generated README at %s: %v", readmePath, err)
	}
}

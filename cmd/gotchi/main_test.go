package main

import (
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/kusold/gotchi/migrations"
)

// ============================================================================
// runInit tests
// ============================================================================

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

func TestRunInitWithEqualsSyntax(t *testing.T) {
	tmp := t.TempDir()
	err := runInit([]string{"testapp", "--module=github.com/test/testapp", "--output=" + tmp})
	if err != nil {
		t.Fatalf("runInit returned error: %v", err)
	}

	goModPath := filepath.Join(tmp, "testapp", "go.mod")
	data, err := os.ReadFile(goModPath)
	if err != nil {
		t.Fatalf("reading go.mod: %v", err)
	}
	if !strings.Contains(string(data), "github.com/test/testapp") {
		t.Error("go.mod should contain custom module path")
	}
}

func TestRunInitDefaultModulePath(t *testing.T) {
	tmp := t.TempDir()
	err := runInit([]string{"myapp", "--output", tmp})
	if err != nil {
		t.Fatalf("runInit returned error: %v", err)
	}

	goModPath := filepath.Join(tmp, "myapp", "go.mod")
	data, err := os.ReadFile(goModPath)
	if err != nil {
		t.Fatalf("reading go.mod: %v", err)
	}
	if !strings.Contains(string(data), "github.com/kusold/myapp") {
		t.Errorf("go.mod should contain default module path github.com/kusold/myapp, got: %s", string(data))
	}
}

func TestRunInitDefaultOutputDir(t *testing.T) {
	// Change to temp dir so output goes there
	origWd, _ := os.Getwd()
	tmp := t.TempDir()
	os.Chdir(tmp)
	defer os.Chdir(origWd)

	err := runInit([]string{"defapp", "--module", "github.com/example/defapp"})
	if err != nil {
		t.Fatalf("runInit returned error: %v", err)
	}

	goModPath := filepath.Join(tmp, "defapp", "go.mod")
	if _, err := os.Stat(goModPath); err != nil {
		t.Fatalf("expected generated go.mod at %s: %v", goModPath, err)
	}
}

func TestRunInitErrors(t *testing.T) {
	tests := []struct {
		name      string
		args      []string
		wantErr   string
	}{
		{
			name:    "missing app name",
			args:    []string{},
			wantErr: "usage:",
		},
		{
			name:    "unknown flag",
			args:    []string{"myapp", "--unknown"},
			wantErr: "unknown init flag",
		},
		{
			name:    "extra argument",
			args:    []string{"myapp", "extra-arg"},
			wantErr: "unexpected extra argument",
		},
		{
			name:    "missing --module value",
			args:    []string{"myapp", "--module"},
			wantErr: "missing value for --module",
		},
		{
			name:    "missing --output value",
			args:    []string{"myapp", "--output"},
			wantErr: "missing value for --output",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := runInit(tt.args)
			if err == nil {
				t.Fatal("expected error")
			}
			if !strings.Contains(err.Error(), tt.wantErr) {
				t.Errorf("expected error containing %q, got: %v", tt.wantErr, err)
			}
		})
	}
}

// ============================================================================
// runAdd tests
// ============================================================================

func TestRunAddCorrelationAudit(t *testing.T) {
	err := runAdd([]string{"feature", "correlation-audit"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestRunAddRequestCorrelation(t *testing.T) {
	err := runAdd([]string{"feature", "request-correlation"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestRunAddAudit(t *testing.T) {
	err := runAdd([]string{"feature", "audit"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestRunAddUnsupportedFeature(t *testing.T) {
	err := runAdd([]string{"feature", "unknown-feature"})
	if err == nil {
		t.Fatal("expected error for unsupported feature")
	}
	if !strings.Contains(err.Error(), "unsupported feature") {
		t.Errorf("expected unsupported feature error, got: %v", err)
	}
}

func TestRunAddMissingFeatureKeyword(t *testing.T) {
	err := runAdd([]string{"something"})
	if err == nil {
		t.Fatal("expected error for missing 'feature' keyword")
	}
	if !strings.Contains(err.Error(), "usage:") {
		t.Errorf("expected usage error, got: %v", err)
	}
}

func TestRunAddMissingFeatureName(t *testing.T) {
	err := runAdd([]string{"feature"})
	if err == nil {
		t.Fatal("expected error for missing feature name")
	}
	if !strings.Contains(err.Error(), "usage:") {
		t.Errorf("expected usage error, got: %v", err)
	}
}

func TestRunAddNoArgs(t *testing.T) {
	err := runAdd([]string{})
	if err == nil {
		t.Fatal("expected error for no args")
	}
}

// ============================================================================
// runDoctor tests
// ============================================================================

func TestRunDoctorSuccess(t *testing.T) {
	// Create a mock project structure
	tmp := t.TempDir()

	// Create go.mod with gotchi dependency
	goModContent := `
module github.com/example/testapp

go 1.22

require github.com/kusold/gotchi v0.1.0
`
	if err := os.WriteFile(filepath.Join(tmp, "go.mod"), []byte(goModContent), 0o644); err != nil {
		t.Fatalf("creating go.mod: %v", err)
	}

	// Create .env.sample with required vars
	envContent := `
DATABASE_URL=postgres://user:pass@localhost:5432/db
PORT=8080
`
	if err := os.WriteFile(filepath.Join(tmp, ".env.sample"), []byte(envContent), 0o644); err != nil {
		t.Fatalf("creating .env.sample: %v", err)
	}

	err := runDoctor([]string{"--root", tmp})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestRunDoctorMissingGoMod(t *testing.T) {
	tmp := t.TempDir()

	// Create .env.sample only
	envContent := `DATABASE_URL=postgres://localhost/db`
	if err := os.WriteFile(filepath.Join(tmp, ".env.sample"), []byte(envContent), 0o644); err != nil {
		t.Fatalf("creating .env.sample: %v", err)
	}

	err := runDoctor([]string{"--root", tmp})
	if err == nil {
		t.Fatal("expected error for missing go.mod")
	}
	if !strings.Contains(err.Error(), "reading go.mod") {
		t.Errorf("expected go.mod error, got: %v", err)
	}
}

func TestRunDoctorMissingEnvSample(t *testing.T) {
	tmp := t.TempDir()

	// Create go.mod only
	goModContent := `module github.com/example/testapp`
	if err := os.WriteFile(filepath.Join(tmp, "go.mod"), []byte(goModContent), 0o644); err != nil {
		t.Fatalf("creating go.mod: %v", err)
	}

	err := runDoctor([]string{"--root", tmp})
	if err == nil {
		t.Fatal("expected error for missing .env.sample")
	}
	if !strings.Contains(err.Error(), "reading .env.sample") {
		t.Errorf("expected .env.sample error, got: %v", err)
	}
}

func TestRunDoctorMissingGotchiDependency(t *testing.T) {
	tmp := t.TempDir()

	// Create go.mod without gotchi
	goModContent := `
module github.com/example/testapp

go 1.22
`
	if err := os.WriteFile(filepath.Join(tmp, "go.mod"), []byte(goModContent), 0o644); err != nil {
		t.Fatalf("creating go.mod: %v", err)
	}

	// Create .env.sample with all required vars
	envContent := `
DATABASE_URL=postgres://localhost/db
PORT=8080
`
	if err := os.WriteFile(filepath.Join(tmp, ".env.sample"), []byte(envContent), 0o644); err != nil {
		t.Fatalf("creating .env.sample: %v", err)
	}

	err := runDoctor([]string{"--root", tmp})
	if err == nil {
		t.Fatal("expected error for missing gotchi dependency")
	}
	if !strings.Contains(err.Error(), "failing checks") {
		t.Errorf("expected failing checks error, got: %v", err)
	}
}

func TestRunDoctorMissingDatabaseURL(t *testing.T) {
	tmp := t.TempDir()

	goModContent := `module github.com/example/testapp
require github.com/kusold/gotchi v0.1.0
`
	if err := os.WriteFile(filepath.Join(tmp, "go.mod"), []byte(goModContent), 0o644); err != nil {
		t.Fatalf("creating go.mod: %v", err)
	}

	// Missing DATABASE_URL
	envContent := `PORT=8080`
	if err := os.WriteFile(filepath.Join(tmp, ".env.sample"), []byte(envContent), 0o644); err != nil {
		t.Fatalf("creating .env.sample: %v", err)
	}

	err := runDoctor([]string{"--root", tmp})
	if err == nil {
		t.Fatal("expected error for missing DATABASE_URL")
	}
}

func TestRunDoctorMissingPort(t *testing.T) {
	tmp := t.TempDir()

	goModContent := `module github.com/example/testapp
require github.com/kusold/gotchi v0.1.0
`
	if err := os.WriteFile(filepath.Join(tmp, "go.mod"), []byte(goModContent), 0o644); err != nil {
		t.Fatalf("creating go.mod: %v", err)
	}

	// Missing PORT
	envContent := `DATABASE_URL=postgres://localhost/db`
	if err := os.WriteFile(filepath.Join(tmp, ".env.sample"), []byte(envContent), 0o644); err != nil {
		t.Fatalf("creating .env.sample: %v", err)
	}

	err := runDoctor([]string{"--root", tmp})
	if err == nil {
		t.Fatal("expected error for missing PORT")
	}
}

func TestRunDoctorDefaultRoot(t *testing.T) {
	// Save and restore working directory
	origWd, _ := os.Getwd()
	tmp := t.TempDir()
	os.Chdir(tmp)
	defer os.Chdir(origWd)

	// Create valid project structure
	goModContent := `module test
require github.com/kusold/gotchi v0.1.0
`
	os.WriteFile("go.mod", []byte(goModContent), 0o644)
	os.WriteFile(".env.sample", []byte("DATABASE_URL=x\nPORT=8080"), 0o644)

	err := runDoctor([]string{})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestRunDoctorInvalidFlag(t *testing.T) {
	err := runDoctor([]string{"--invalid"})
	if err == nil {
		t.Fatal("expected error for invalid flag")
	}
}

// ============================================================================
// runMigrations tests
// ============================================================================

func TestRunMigrationsNoArgs(t *testing.T) {
	err := runMigrations([]string{})
	if err == nil {
		t.Fatal("expected error for no args")
	}
	if !strings.Contains(err.Error(), "usage:") {
		t.Errorf("expected usage error, got: %v", err)
	}
}

func TestRunMigrationsUnknownSubcommand(t *testing.T) {
	err := runMigrations([]string{"import"})
	if err == nil {
		t.Fatal("expected error for unknown subcommand")
	}
	if !strings.Contains(err.Error(), "unknown migrations subcommand") {
		t.Errorf("expected unknown subcommand error, got: %v", err)
	}
}

// ============================================================================
// runMigrationsExport tests
// ============================================================================

func TestMigrationsExportAll(t *testing.T) {
	tmp := t.TempDir()
	err := runMigrationsExport([]string{"--output", tmp})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Should have core/ and auth/ subdirectories with SQL files
	coreFiles, _ := filepath.Glob(filepath.Join(tmp, "core", "*.sql"))
	authFiles, _ := filepath.Glob(filepath.Join(tmp, "auth", "*.sql"))
	if len(coreFiles) == 0 {
		t.Error("expected at least one core migration file")
	}
	if len(authFiles) == 0 {
		t.Error("expected at least one auth migration file")
	}
}

func TestMigrationsExportComponentCore(t *testing.T) {
	tmp := t.TempDir()
	err := runMigrationsExport([]string{"--output", tmp, "--component", "core"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	coreFiles, _ := filepath.Glob(filepath.Join(tmp, "core", "*.sql"))
	if len(coreFiles) == 0 {
		t.Error("expected core migration files")
	}

	// auth directory should not exist
	if _, err := os.Stat(filepath.Join(tmp, "auth")); !os.IsNotExist(err) {
		t.Error("auth directory should not exist when --component=core")
	}
}

func TestMigrationsExportComponentAuth(t *testing.T) {
	tmp := t.TempDir()
	err := runMigrationsExport([]string{"--output", tmp, "--component", "auth"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	authFiles, _ := filepath.Glob(filepath.Join(tmp, "auth", "*.sql"))
	if len(authFiles) == 0 {
		t.Error("expected auth migration files")
	}

	// core directory should not exist
	if _, err := os.Stat(filepath.Join(tmp, "core")); !os.IsNotExist(err) {
		t.Error("core directory should not exist when --component=auth")
	}
}

func TestMigrationsExportIdempotentUnchanged(t *testing.T) {
	tmp := t.TempDir()

	// First export
	if err := runMigrationsExport([]string{"--output", tmp}); err != nil {
		t.Fatalf("first export: %v", err)
	}

	// Second export should succeed and report unchanged
	if err := runMigrationsExport([]string{"--output", tmp}); err != nil {
		t.Fatalf("second export: %v", err)
	}
}

func TestMigrationsExportConflictErrors(t *testing.T) {
	tmp := t.TempDir()

	// Create a file that will conflict
	coreDir := filepath.Join(tmp, "core")
	if err := os.MkdirAll(coreDir, 0o755); err != nil {
		t.Fatal(err)
	}
	conflictPath := filepath.Join(coreDir, "20260221160000_core.sql")
	if err := os.WriteFile(conflictPath, []byte("different content"), 0o644); err != nil {
		t.Fatal(err)
	}

	err := runMigrationsExport([]string{"--output", tmp})
	if err == nil {
		t.Fatal("expected error for conflicting file")
	}
	if !strings.Contains(err.Error(), "already exists with different content") {
		t.Errorf("expected conflict error, got: %v", err)
	}
}

func TestMigrationsExportForceOverwrites(t *testing.T) {
	tmp := t.TempDir()

	// Create conflicting file
	coreDir := filepath.Join(tmp, "core")
	if err := os.MkdirAll(coreDir, 0o755); err != nil {
		t.Fatal(err)
	}
	conflictPath := filepath.Join(coreDir, "20260221160000_core.sql")
	if err := os.WriteFile(conflictPath, []byte("old content"), 0o644); err != nil {
		t.Fatal(err)
	}

	err := runMigrationsExport([]string{"--output", tmp, "--force"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	data, err := os.ReadFile(conflictPath)
	if err != nil {
		t.Fatal(err)
	}
	if string(data) == "old content" {
		t.Error("expected file to be overwritten with embedded content")
	}
}

func TestMigrationsExportSkipConflicts(t *testing.T) {
	tmp := t.TempDir()

	// Create conflicting file
	coreDir := filepath.Join(tmp, "core")
	if err := os.MkdirAll(coreDir, 0o755); err != nil {
		t.Fatal(err)
	}
	conflictPath := filepath.Join(coreDir, "20260221160000_core.sql")
	if err := os.WriteFile(conflictPath, []byte("old content"), 0o644); err != nil {
		t.Fatal(err)
	}

	err := runMigrationsExport([]string{"--output", tmp, "--skip"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	data, err := os.ReadFile(conflictPath)
	if err != nil {
		t.Fatal(err)
	}
	if string(data) != "old content" {
		t.Error("expected conflicting file to be preserved when using --skip")
	}
}

func TestMigrationsExportForceAndSkipRejected(t *testing.T) {
	err := runMigrationsExport([]string{"--force", "--skip"})
	if err == nil {
		t.Fatal("expected error for --force and --skip together")
	}
	if !strings.Contains(err.Error(), "cannot use both --force and --skip") {
		t.Errorf("expected mutual exclusion error, got: %v", err)
	}
}

func TestMigrationsExportInvalidComponent(t *testing.T) {
	err := runMigrationsExport([]string{"--component", "bogus"})
	if err == nil {
		t.Fatal("expected error for invalid component")
	}
	if !strings.Contains(err.Error(), "invalid --component value") {
		t.Errorf("expected invalid component error, got: %v", err)
	}
}

func TestMigrationsExportShortOutputFlag(t *testing.T) {
	tmp := t.TempDir()
	err := runMigrationsExport([]string{"-o", tmp})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	coreFiles, _ := filepath.Glob(filepath.Join(tmp, "core", "*.sql"))
	if len(coreFiles) == 0 {
		t.Error("expected migration files with -o shorthand")
	}
}

func TestMigrationsExportOutputMatchesEmbedded(t *testing.T) {
	tmp := t.TempDir()
	if err := runMigrationsExport([]string{"--output", tmp, "--component", "core"}); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Read the embedded content directly and compare with exported file
	embedded, err := fs.ReadFile(migrations.Core(), "20260221160000_core.sql")
	if err != nil {
		t.Fatalf("reading embedded file: %v", err)
	}
	exported, err := os.ReadFile(filepath.Join(tmp, "core", "20260221160000_core.sql"))
	if err != nil {
		t.Fatalf("reading exported file: %v", err)
	}
	if string(embedded) != string(exported) {
		t.Error("exported file content does not match embedded content")
	}
}

// ============================================================================
// printUsage test (simple coverage)
// ============================================================================

func TestPrintUsage(t *testing.T) {
	// Just call it to ensure coverage - it prints to stdout
	printUsage()
}

package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
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

func TestRunInitMissingAppName(t *testing.T) {
	err := runInit([]string{})
	if err == nil {
		t.Fatal("expected error for missing app name")
	}
	if !strings.Contains(err.Error(), "usage:") {
		t.Errorf("expected usage error, got: %v", err)
	}
}

func TestRunInitUnknownFlag(t *testing.T) {
	err := runInit([]string{"myapp", "--unknown"})
	if err == nil {
		t.Fatal("expected error for unknown flag")
	}
	if !strings.Contains(err.Error(), "unknown init flag") {
		t.Errorf("expected unknown flag error, got: %v", err)
	}
}

func TestRunInitExtraArgument(t *testing.T) {
	err := runInit([]string{"myapp", "extra-arg"})
	if err == nil {
		t.Fatal("expected error for extra argument")
	}
	if !strings.Contains(err.Error(), "unexpected extra argument") {
		t.Errorf("expected extra argument error, got: %v", err)
	}
}

func TestRunInitModuleFlagMissingValue(t *testing.T) {
	err := runInit([]string{"myapp", "--module"})
	if err == nil {
		t.Fatal("expected error for missing --module value")
	}
	if !strings.Contains(err.Error(), "missing value for --module") {
		t.Errorf("expected missing module value error, got: %v", err)
	}
}

func TestRunInitOutputFlagMissingValue(t *testing.T) {
	err := runInit([]string{"myapp", "--output"})
	if err == nil {
		t.Fatal("expected error for missing --output value")
	}
	if !strings.Contains(err.Error(), "missing value for --output") {
		t.Errorf("expected missing output value error, got: %v", err)
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
// printUsage test (simple coverage)
// ============================================================================

func TestPrintUsage(t *testing.T) {
	// Just call it to ensure coverage - it prints to stdout
	printUsage()
}

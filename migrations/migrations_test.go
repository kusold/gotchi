package migrations

import (
	"io/fs"
	"strings"
	"testing"
)

func TestCoreReturnsValidFS(t *testing.T) {
	core := Core()
	if core == nil {
		t.Fatal("Core() returned nil")
	}

	entries, err := fs.ReadDir(core, ".")
	if err != nil {
		t.Fatalf("failed to read Core() FS: %v", err)
	}

	if len(entries) == 0 {
		t.Fatal("Core() FS has no entries")
	}

	for _, e := range entries {
		if !strings.HasSuffix(e.Name(), ".sql") {
			t.Errorf("Core() FS contains non-.sql file: %s", e.Name())
		}
	}
}

func TestAuthReturnsValidFS(t *testing.T) {
	auth := Auth()
	if auth == nil {
		t.Fatal("Auth() returned nil")
	}

	entries, err := fs.ReadDir(auth, ".")
	if err != nil {
		t.Fatalf("failed to read Auth() FS: %v", err)
	}

	if len(entries) == 0 {
		t.Fatal("Auth() FS has no entries")
	}

	for _, e := range entries {
		if !strings.HasSuffix(e.Name(), ".sql") {
			t.Errorf("Auth() FS contains non-.sql file: %s", e.Name())
		}
	}
}

func TestCoreFilesReadable(t *testing.T) {
	core := Core()

	err := fs.WalkDir(core, ".", func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			return nil
		}
		data, err := fs.ReadFile(core, path)
		if err != nil {
			t.Errorf("cannot read %s: %v", path, err)
			return nil
		}
		if len(data) == 0 {
			t.Errorf("file %s is empty", path)
		}
		return nil
	})
	if err != nil {
		t.Fatalf("WalkDir failed: %v", err)
	}
}

func TestAuthFilesReadable(t *testing.T) {
	auth := Auth()

	err := fs.WalkDir(auth, ".", func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			return nil
		}
		data, err := fs.ReadFile(auth, path)
		if err != nil {
			t.Errorf("cannot read %s: %v", path, err)
			return nil
		}
		if len(data) == 0 {
			t.Errorf("file %s is empty", path)
		}
		return nil
	})
	if err != nil {
		t.Fatalf("WalkDir failed: %v", err)
	}
}

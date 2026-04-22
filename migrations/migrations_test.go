package migrations

import (
	"io/fs"
	"strings"
	"testing"
)

func TestMigrationFSReturnsValidFS(t *testing.T) {
	tests := []struct {
		name string
		get  func() fs.FS
	}{
		{"Core", Core},
		{"Auth", Auth},
		{"Password", Password},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			fsys := tt.get()
			if fsys == nil {
				t.Fatal("returned nil")
			}

			entries, err := fs.ReadDir(fsys, ".")
			if err != nil {
				t.Fatalf("failed to read FS: %v", err)
			}

			if len(entries) == 0 {
				t.Fatal("FS has no entries")
			}

			for _, e := range entries {
				if !strings.HasSuffix(e.Name(), ".sql") {
					t.Errorf("FS contains non-.sql file: %s", e.Name())
				}
			}
		})
	}
}

func TestMigrationFSFilesReadable(t *testing.T) {
	tests := []struct {
		name string
		get  func() fs.FS
	}{
		{"Core", Core},
		{"Auth", Auth},
		{"Password", Password},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			fsys := tt.get()

			err := fs.WalkDir(fsys, ".", func(path string, d fs.DirEntry, err error) error {
				if err != nil {
					return err
				}
				if d.IsDir() {
					return nil
				}
				data, err := fs.ReadFile(fsys, path)
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
		})
	}
}

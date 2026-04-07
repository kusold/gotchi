package migrations

import (
	"embed"
	"io/fs"
)

//go:embed core/*.sql auth/*.sql
var migrationFS embed.FS

func subFS(dir string) fs.FS {
	sub, err := fs.Sub(migrationFS, dir)
	if err != nil {
		panic(err)
	}
	return sub
}

func Core() fs.FS {
	return subFS("core")
}

func Auth() fs.FS {
	return subFS("auth")
}

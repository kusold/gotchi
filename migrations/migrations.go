package migrations

import (
	"embed"
	"io/fs"
)

//go:embed core/*.sql auth/*.sql
var migrationFS embed.FS

func Core() fs.FS {
	sub, err := fs.Sub(migrationFS, "core")
	if err != nil {
		panic(err)
	}
	return sub
}

func Auth() fs.FS {
	sub, err := fs.Sub(migrationFS, "auth")
	if err != nil {
		panic(err)
	}
	return sub
}

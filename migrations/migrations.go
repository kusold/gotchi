// Package migrations provides embedded SQL migration files for gotchi's core
// schema and authentication schema. Each function returns an [fs.FS] that can be
// passed to [db.Manager.AddMigrationSource].
//
// The migrations use the goose migration format with SQL files embedded at
// compile time via [embed.FS]. Two migration sets are provided:
//
//   - [Core]: creates the foundational multi-tenant schema (tenants, users,
//     memberships, etc.).
//   - [Auth]: creates the OIDC authentication tables and related schema.
//
// # Usage with the db package
//
// Pass the returned filesystems to the database manager before connecting:
//
//	dbMgr := db.NewManager(dbConfig)
//	dbMgr.AddMigrationSource(db.MigrationSource{
//	    FS:  migrations.Core(),
//	    Dir: ".",
//	})
//	dbMgr.AddMigrationSource(db.MigrationSource{
//	    FS:  migrations.Auth(),
//	    Dir: ".",
//	})
package migrations

import (
	"embed"
	"io/fs"
)

// migrationFS holds the embedded SQL migration files from the core/ and auth/
// subdirectories. The go:embed directive includes all .sql files at build time.
//
//go:embed core/*.sql auth/*.sql
var migrationFS embed.FS

// subFS returns a sub-filesystem rooted at dir within migrationFS.
func subFS(dir string) fs.FS {
	sub, err := fs.Sub(migrationFS, dir)
	if err != nil {
		panic(err)
	}
	return sub
}

// Core returns the embedded filesystem containing core gotchi database
// migrations. These migrations create the foundational multi-tenant schema
// including tenants, users, and membership tables.
func Core() fs.FS {
	return subFS("core")
}

// Auth returns the embedded filesystem containing authentication-related
// database migrations. These migrations create OIDC identity tables and
// related auth schema extensions.
func Auth() fs.FS {
	return subFS("auth")
}

// Package db provides database connection management for gotchi applications
// with built-in support for multi-tenancy, schema migrations, and OpenTelemetry
// tracing.
//
// The [Manager] type handles the lifecycle of a PostgreSQL connection pool
// ([pgxpool.Pool]) with automatic tenant isolation via the [tenantctx] package.
// When a connection is acquired from the pool, it automatically sets the tenant
// context using the PostgreSQL set_tenant function if one is present in the
// request context.
//
// # Quick Start
//
// Create a manager, register migrations, connect, and run them:
//
//	dbMgr := db.NewManager(db.Config{
//	    DatabaseURL: "postgres://user:pass@localhost:5432/mydb",
//	})
//	dbMgr.AddMigrationSource(db.MigrationSource{
//	    FS:  migrations.Core(),
//	    Dir: ".",
//	})
//	if err := dbMgr.Connect(ctx); err != nil {
//	    log.Fatal(err)
//	}
//	if err := dbMgr.RunMigrations(ctx); err != nil {
//	    log.Fatal(err)
//	}
//
// # Multi-Tenancy
//
// Connections automatically apply tenant isolation based on the tenant ID stored
// in the request context via [tenantctx.WithTenantID]. Use [AdminContext] for
// operations that should bypass tenant filtering (e.g., migrations).
package db

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"log/slog"
	"sort"
	"time"

	"github.com/exaring/otelpgx"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/jackc/pgx/v5/stdlib"
	"github.com/jackc/pgx/v5/tracelog"
	pgxslog "github.com/mcosta74/pgx-slog"
	"github.com/pressly/goose/v3"

	"github.com/kusold/gotchi/tenantctx"
)

// Config holds the settings for connecting to a PostgreSQL database.
type Config struct {
	// DatabaseURL is the PostgreSQL connection string (e.g.
	// "postgres://user:pass@host:5432/dbname?sslmode=disable"). Required.
	DatabaseURL string
	// SearchPath optionally sets the PostgreSQL search_path for all connections.
	// This is primarily used for migration tooling.
	SearchPath string
	// EnableSlogTracing enables pgx query logging via slog at trace level.
	EnableSlogTracing bool
	// OTELTracing enables OpenTelemetry tracing for database queries via otelpgx.
	OTELTracing bool
}

// MigrationSource describes a set of SQL migration files to apply. FS is an
// [fs.FS] containing goose-formatted .sql files, and Dir is the subdirectory
// within FS to use (defaults to "." when empty).
type MigrationSource struct {
	FS  fs.FS
	Dir string
}

// Manager manages the lifecycle of a PostgreSQL connection pool with support
// for multi-tenancy and migrations. Create one with [NewManager], register
// migration sources, then call [Manager.Connect] to establish the pool.
type Manager struct {
	cfg            Config
	pool           *pgxpool.Pool
	migrationFiles []MigrationSource
}

// NewManager creates a new database Manager with the given configuration.
// The returned Manager has no pool until [Manager.Connect] is called.
func NewManager(cfg Config) *Manager {
	return &Manager{cfg: cfg}
}

// AddMigrationSource registers a migration source to be applied when
// [Manager.RunMigrations] is called. If source.Dir is empty it defaults to ".".
func (m *Manager) AddMigrationSource(source MigrationSource) {
	if source.Dir == "" {
		source.Dir = "."
	}
	m.migrationFiles = append(m.migrationFiles, source)
}

// Connect establishes the connection pool using the configured DatabaseURL.
// If a pool already exists it returns nil immediately. After connecting it
// pings the database using an [AdminContext] to verify connectivity.
func (m *Manager) Connect(ctx context.Context) error {
	if m.pool != nil {
		return nil
	}

	parsedCfg, err := pgxpool.ParseConfig(m.cfg.DatabaseURL)
	if err != nil {
		return err
	}

	if m.cfg.EnableSlogTracing || m.cfg.OTELTracing {
		parsedCfg = setupTracing(parsedCfg, m.cfg)
	}
	if m.cfg.SearchPath != "" {
		parsedCfg.ConnConfig.RuntimeParams["search_path"] = m.cfg.SearchPath
	}
	parsedCfg = m.setupMultitenancy(parsedCfg)

	pool, err := pgxpool.NewWithConfig(ctx, parsedCfg)
	if err != nil {
		return err
	}
	m.pool = pool

	return m.Ping(AdminContext(ctx))
}

// EnableOTELTracing enables OpenTelemetry tracing on the database connections.
// This must be called before [Manager.Connect] to take effect.
func (m *Manager) EnableOTELTracing() {
	m.cfg.OTELTracing = true
}

// Close shuts down the connection pool and releases all resources.
func (m *Manager) Close() error {
	if m.pool != nil {
		m.pool.Close()
	}
	return nil
}

// Ping verifies connectivity to the database. Returns an error if the pool
// has not been initialized.
func (m *Manager) Ping(ctx context.Context) error {
	if m.pool == nil {
		return fmt.Errorf("database pool is not initialized")
	}
	return m.pool.Ping(ctx)
}

// Pool returns the underlying pgxpool.Pool. Returns nil if [Manager.Connect]
// has not been called.
func (m *Manager) Pool() *pgxpool.Pool {
	return m.pool
}

// RunMigrations applies all registered migration sources using goose. All
// sources are merged into a single flat filesystem sorted by filename (i.e.
// by migration version) before being applied, so interleaved timestamps across
// sources are handled correctly. Migrations are run using an [AdminContext] to
// bypass tenant isolation. Returns nil immediately if no migration sources are
// registered.
func (m *Manager) RunMigrations(ctx context.Context) error {
	if m.pool == nil {
		return fmt.Errorf("database pool is not initialized")
	}
	if len(m.migrationFiles) == 0 {
		return nil
	}

	if err := goose.SetDialect("postgres"); err != nil {
		return err
	}

	merged, err := mergeMigrationSources(m.migrationFiles)
	if err != nil {
		return fmt.Errorf("merging migration sources: %w", err)
	}

	sqlDB := stdlib.OpenDBFromPool(m.pool)
	defer sqlDB.Close()

	goose.SetBaseFS(merged)
	return goose.UpContext(AdminContext(ctx), sqlDB, ".")
}

// mergeMigrationSources combines all migration source filesystems into a
// single flat [fs.FS]. Files are collected from each source and keyed by
// filename; duplicate filenames across sources are an error. The resulting
// filesystem presents all files in a single root directory (".").
func mergeMigrationSources(sources []MigrationSource) (fs.FS, error) {
	files := make(map[string][]byte)
	for _, src := range sources {
		entries, err := fs.ReadDir(src.FS, src.Dir)
		if err != nil {
			return nil, fmt.Errorf("listing migrations in %q: %w", src.Dir, err)
		}
		for _, e := range entries {
			if e.IsDir() {
				continue
			}
			name := e.Name()
			filePath := name
			if src.Dir != "." && src.Dir != "" {
				filePath = src.Dir + "/" + name
			}
			data, err := fs.ReadFile(src.FS, filePath)
			if err != nil {
				return nil, fmt.Errorf("reading migration %s: %w", name, err)
			}
			if _, dup := files[name]; dup {
				return nil, fmt.Errorf("duplicate migration filename: %s", name)
			}
			files[name] = data
		}
	}
	return &flatFS{files: files}, nil
}

// flatFS is a read-only in-memory filesystem that presents a map of filename
// → content as a single flat directory. It implements [fs.FS] and
// [fs.ReadDirFS] so that goose can list and read migration files.
type flatFS struct {
	files map[string][]byte
}

func (f *flatFS) Open(name string) (fs.File, error) {
	if name == "." {
		return f.openRoot(), nil
	}
	data, ok := f.files[name]
	if !ok {
		return nil, &fs.PathError{Op: "open", Path: name, Err: fs.ErrNotExist}
	}
	return &flatFile{
		name:   name,
		Reader: bytes.NewReader(data),
		size:   int64(len(data)),
	}, nil
}

func (f *flatFS) openRoot() *flatDir {
	names := make([]string, 0, len(f.files))
	for n := range f.files {
		names = append(names, n)
	}
	sort.Strings(names)
	entries := make([]fs.DirEntry, len(names))
	for i, n := range names {
		entries[i] = &flatDirEntry{name: n, size: int64(len(f.files[n]))}
	}
	return &flatDir{entries: entries}
}

// flatFile represents a single file in a [flatFS].
type flatFile struct {
	name string
	size int64
	*bytes.Reader
}

func (f *flatFile) Close() error               { return nil }
func (f *flatFile) Stat() (fs.FileInfo, error) { return &flatFileInfo{name: f.name, size: f.size}, nil }

// flatDir represents the root directory of a [flatFS].
type flatDir struct {
	entries []fs.DirEntry
	pos     int
}

func (d *flatDir) Stat() (fs.FileInfo, error) { return &flatDirInfo{}, nil }
func (d *flatDir) Read([]byte) (int, error)   { return 0, io.EOF }
func (d *flatDir) Close() error               { return nil }
func (d *flatDir) ReadDir(n int) ([]fs.DirEntry, error) {
	if d.pos >= len(d.entries) {
		if n <= 0 {
			return nil, nil
		}
		return nil, io.EOF
	}
	if n <= 0 {
		all := d.entries[d.pos:]
		d.pos = len(d.entries)
		return all, nil
	}
	end := d.pos + n
	if end > len(d.entries) {
		end = len(d.entries)
	}
	result := d.entries[d.pos:end]
	d.pos = end
	return result, nil
}

// flatDirEntry is an [fs.DirEntry] for a regular file in a [flatFS].
type flatDirEntry struct {
	name string
	size int64
}

func (e *flatDirEntry) Name() string      { return e.name }
func (e *flatDirEntry) IsDir() bool       { return false }
func (e *flatDirEntry) Type() fs.FileMode { return 0 }
func (e *flatDirEntry) Info() (fs.FileInfo, error) {
	return &flatFileInfo{name: e.name, size: e.size}, nil
}

// flatFileInfo is an [fs.FileInfo] for a regular file in a [flatFS].
type flatFileInfo struct {
	name string
	size int64
}

func (i *flatFileInfo) Name() string       { return i.name }
func (i *flatFileInfo) Size() int64        { return i.size }
func (i *flatFileInfo) Mode() fs.FileMode  { return 0o444 }
func (i *flatFileInfo) ModTime() time.Time { return time.Time{} }
func (i *flatFileInfo) IsDir() bool        { return false }
func (i *flatFileInfo) Sys() any           { return nil }

// flatDirInfo is an [fs.FileInfo] for the root directory of a [flatFS].
type flatDirInfo struct{}

func (i *flatDirInfo) Name() string       { return "." }
func (i *flatDirInfo) Size() int64        { return 0 }
func (i *flatDirInfo) Mode() fs.FileMode  { return fs.ModeDir | 0o555 }
func (i *flatDirInfo) ModTime() time.Time { return time.Time{} }
func (i *flatDirInfo) IsDir() bool        { return true }
func (i *flatDirInfo) Sys() any           { return nil }

// AdminContext returns a context with the system tenant set, bypassing
// tenant isolation. Use this for administrative operations like migrations
// or internal queries that need access to all tenant data.
func AdminContext(ctx context.Context) context.Context {
	if ctx == nil {
		ctx = context.Background()
	}
	return tenantctx.WithSystemTenant(ctx)
}

func (m *Manager) setupMultitenancy(cfg *pgxpool.Config) *pgxpool.Config {
	setTenantSQL := "select set_tenant($1)"

	cfg.BeforeAcquire = func(ctx context.Context, conn *pgx.Conn) bool {
		tenantID, ok := tenantctx.TenantIDString(ctx)
		if !ok {
			return true
		}
		if tenantID == tenantctx.SystemTenant {
			return true
		}
		if _, err := conn.Exec(ctx, setTenantSQL, tenantID); err != nil {
			if isUndefinedFunctionError(err) {
				// Tolerate during initial migrations when set_tenant doesn't exist yet
				slog.Warn("set_tenant function not found, tenant not set on connection", "err", err)
				return true
			}
			slog.Error("failed to set tenant on postgres connection", "err", err)
			return false
		}
		return true
	}

	cfg.AfterRelease = func(conn *pgx.Conn) bool {
		if _, err := conn.Exec(context.Background(), setTenantSQL, ""); err != nil {
			if isUndefinedFunctionError(err) {
				// Tolerate during initial migrations when set_tenant doesn't exist yet
				slog.Warn("set_tenant function not found, tenant not cleared on connection", "err", err)
				return true
			}
			slog.Error("failed to clear tenant on postgres connection", "err", err)
			return false
		}
		return true
	}

	return cfg
}

// isUndefinedFunctionError returns true if the error is a PostgreSQL
// "undefined_function" error (SQLSTATE 42883). This occurs when set_tenant()
// hasn't been created yet, e.g. during initial migrations.
func isUndefinedFunctionError(err error) bool {
	var pgErr *pgconn.PgError
	return errors.As(err, &pgErr) && pgErr.Code == "42883"
}

func setupTracing(cfg *pgxpool.Config, dbCfg Config) *pgxpool.Config {
	var tracers []pgx.QueryTracer

	if dbCfg.EnableSlogTracing {
		logger := pgxslog.NewLogger(slog.Default())
		tracers = append(tracers, &tracelog.TraceLog{
			Logger:   logger,
			LogLevel: tracelog.LogLevelTrace,
		})
	}

	if dbCfg.OTELTracing {
		tracers = append(tracers, otelpgx.NewTracer())
	}

	if len(tracers) == 1 {
		cfg.ConnConfig.Tracer = tracers[0]
	} else if len(tracers) > 1 {
		cfg.ConnConfig.Tracer = &multiTracer{tracers: tracers}
	}

	return cfg
}

type multiTracer struct {
	tracers []pgx.QueryTracer
}

func (mt *multiTracer) TraceQueryStart(ctx context.Context, conn *pgx.Conn, data pgx.TraceQueryStartData) context.Context {
	for _, t := range mt.tracers {
		ctx = t.TraceQueryStart(ctx, conn, data)
	}
	return ctx
}

func (mt *multiTracer) TraceQueryEnd(ctx context.Context, conn *pgx.Conn, data pgx.TraceQueryEndData) {
	for i := len(mt.tracers) - 1; i >= 0; i-- {
		mt.tracers[i].TraceQueryEnd(ctx, conn, data)
	}
}

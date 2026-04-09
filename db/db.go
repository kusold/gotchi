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
	"context"
	"errors"
	"fmt"
	"io/fs"
	"log/slog"

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

// RunMigrations applies all registered migration sources in order using
// goose. Migrations are run using an [AdminContext] to bypass tenant
// isolation. Returns nil immediately if no migration sources are registered.
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

	sqlDB := stdlib.OpenDBFromPool(m.pool)
	defer sqlDB.Close()

	for _, source := range m.migrationFiles {
		goose.SetBaseFS(source.FS)
		if err := goose.UpContext(AdminContext(ctx), sqlDB, source.Dir); err != nil {
			return err
		}
	}
	return nil
}

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

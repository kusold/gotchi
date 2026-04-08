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

type Config struct {
	DatabaseURL   string
	SearchPath    string
	EnableTracing bool
	OTELTracing   bool
}

type MigrationSource struct {
	FS  fs.FS
	Dir string
}

type Manager struct {
	cfg            Config
	pool           *pgxpool.Pool
	migrationFiles []MigrationSource
}

func NewManager(cfg Config) *Manager {
	return &Manager{cfg: cfg}
}

func (m *Manager) AddMigrationSource(source MigrationSource) {
	if source.Dir == "" {
		source.Dir = "."
	}
	m.migrationFiles = append(m.migrationFiles, source)
}

func (m *Manager) Connect(ctx context.Context) error {
	if m.pool != nil {
		return nil
	}

	parsedCfg, err := pgxpool.ParseConfig(m.cfg.DatabaseURL)
	if err != nil {
		return err
	}

	if m.cfg.EnableTracing || m.cfg.OTELTracing {
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

func (m *Manager) Close() {
	if m.pool != nil {
		m.pool.Close()
	}
}

func (m *Manager) Ping(ctx context.Context) error {
	if m.pool == nil {
		return fmt.Errorf("database pool is not initialized")
	}
	return m.pool.Ping(ctx)
}

func (m *Manager) Pool() *pgxpool.Pool {
	return m.pool
}

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

	if dbCfg.EnableTracing {
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

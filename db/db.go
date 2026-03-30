package db

import (
	"context"
	"fmt"
	"io/fs"
	"log/slog"
	"strings"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/jackc/pgx/v5/stdlib"
	"github.com/jackc/pgx/v5/tracelog"
	pgxslog "github.com/mcosta74/pgx-slog"
	"github.com/pressly/goose/v3"

	"github.com/kusold/gotchi/tenantctx"
)

type Config struct {
	DatabaseURL   string
	Schema        string
	EnableTracing bool
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

	if m.cfg.Schema != "" {
		if parsedCfg.ConnConfig.RuntimeParams == nil {
			parsedCfg.ConnConfig.RuntimeParams = map[string]string{}
		}
		parsedCfg.ConnConfig.RuntimeParams["search_path"] = fmt.Sprintf("%s,public", m.cfg.Schema)
	}

	if m.cfg.EnableTracing {
		parsedCfg = setupTracing(parsedCfg)
	}
	parsedCfg = m.setupMultitenancy(parsedCfg)

	pool, err := pgxpool.NewWithConfig(ctx, parsedCfg)
	if err != nil {
		return err
	}
	m.pool = pool

	if m.cfg.Schema != "" {
		query := fmt.Sprintf("CREATE SCHEMA IF NOT EXISTS %s", quoteIdentifier(m.cfg.Schema))
		if _, err := m.pool.Exec(AdminContext(ctx), query); err != nil {
			return err
		}
	}

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
	if m.cfg.Schema != "" {
		setTenantSQL = fmt.Sprintf("select %s.set_tenant($1)", quoteIdentifier(m.cfg.Schema))
	}

	cfg.BeforeAcquire = func(ctx context.Context, conn *pgx.Conn) bool {
		tenantID, ok := tenantctx.TenantIDString(ctx)
		if !ok {
			return true
		}
		if tenantID == tenantctx.SystemTenant {
			return true
		}
		if _, err := conn.Exec(ctx, setTenantSQL, tenantID); err != nil {
			// Log the error but don't fail the acquire
			// The function may not exist yet during initial migrations
			slog.Debug("failed to set tenant on postgres connection", "err", err)
		}
		return true
	}

	cfg.AfterRelease = func(conn *pgx.Conn) bool {
		if _, err := conn.Exec(context.Background(), setTenantSQL, ""); err != nil {
			// Log the error but don't destroy the connection
			// The function may not exist yet during initial migrations
			slog.Debug("failed to clear tenant on postgres connection", "err", err)
		}
		return true
	}

	return cfg
}

func setupTracing(cfg *pgxpool.Config) *pgxpool.Config {
	logger := pgxslog.NewLogger(slog.Default())
	cfg.ConnConfig.Tracer = &tracelog.TraceLog{
		Logger:   logger,
		LogLevel: tracelog.LogLevelTrace,
	}
	return cfg
}

func quoteIdentifier(value string) string {
	escaped := strings.ReplaceAll(value, `"`, `""`)
	return `"` + escaped + `"`
}

package main

import (
	"context"
	"flag"
	"fmt"
	"io/fs"
	"log"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/jackc/pgx/v5/stdlib"
	"github.com/pressly/goose/v3"

	"github.com/kusold/gotchi/db"
	"github.com/kusold/gotchi/migrations"
)

type migrationSource struct {
	name string
	fs   fs.FS
	dir  string
}

var (
	expectedTables    = []string{"sessions", "tenants", "users", "tenant_memberships"}
	expectedFunctions = []string{"set_tenant"}
)

func main() {
	databaseURL := flag.String("database-url", "", "Postgres connection URL")
	schema := flag.String("schema", "", "Target schema for migration checks (defaults to a temporary schema)")
	flag.Parse()

	if *databaseURL == "" {
		log.Fatal("database URL is required (pass -database-url or set DATABASE_URL in wrapper script)")
	}

	targetSchema := *schema
	if targetSchema == "" {
		targetSchema = buildTemporarySchemaName()
	}

	ctx := context.Background()
	manager := db.NewManager(db.Config{
		DatabaseURL: *databaseURL,
	})

	if err := manager.Connect(ctx); err != nil {
		log.Fatalf("connect failed: %v", err)
	}
	defer manager.Close()

	adminCtx := db.AdminContext(ctx)
	defer dropSchema(adminCtx, manager.Pool(), targetSchema)

	// Create the target schema and set search_path
	if _, err := manager.Pool().Exec(adminCtx, fmt.Sprintf("CREATE SCHEMA IF NOT EXISTS %q", targetSchema)); err != nil {
		log.Fatalf("create schema failed: %v", err)
	}
	if _, err := manager.Pool().Exec(adminCtx, fmt.Sprintf("SET search_path TO %q, public", targetSchema)); err != nil {
		log.Fatalf("set search_path failed: %v", err)
	}

	sources := []migrationSource{
		{name: "core", fs: migrations.Core(), dir: "."},
		{name: "auth", fs: migrations.Auth(), dir: "."},
	}

	for _, source := range sources {
		manager.AddMigrationSource(db.MigrationSource{FS: source.fs, Dir: source.dir})
	}

	log.Printf("migration regression: applying up migrations on schema %q", targetSchema)
	if err := manager.RunMigrations(adminCtx); err != nil {
		log.Fatalf("initial up failed: %v", err)
	}
	if err := assertObjectsPresent(adminCtx, manager.Pool(), targetSchema); err != nil {
		log.Fatalf("post-up verification failed: %v", err)
	}

	log.Printf("migration regression: applying down migrations")
	if err := runDownMigrations(adminCtx, manager.Pool(), sources); err != nil {
		log.Fatalf("down failed: %v", err)
	}
	if err := assertObjectsAbsent(adminCtx, manager.Pool(), targetSchema); err != nil {
		log.Fatalf("post-down verification failed: %v", err)
	}

	log.Printf("migration regression: applying up migrations again")
	if err := manager.RunMigrations(adminCtx); err != nil {
		log.Fatalf("second up failed: %v", err)
	}
	if err := assertObjectsPresent(adminCtx, manager.Pool(), targetSchema); err != nil {
		log.Fatalf("post-second-up verification failed: %v", err)
	}

	log.Printf("migration regression passed")
}

func runDownMigrations(ctx context.Context, pool *pgxpool.Pool, sources []migrationSource) error {
	if err := goose.SetDialect("postgres"); err != nil {
		return err
	}

	sqlDB := stdlib.OpenDBFromPool(pool)
	defer sqlDB.Close()

	for i := len(sources) - 1; i >= 0; i-- {
		source := sources[i]
		goose.SetBaseFS(source.fs)
		if err := goose.ResetContext(ctx, sqlDB, source.dir); err != nil {
			return fmt.Errorf("%s reset: %w", source.name, err)
		}
	}
	return nil
}

func assertObjectsPresent(ctx context.Context, pool *pgxpool.Pool, schema string) error {
	for _, table := range expectedTables {
		exists, err := tableExists(ctx, pool, schema, table)
		if err != nil {
			return err
		}
		if !exists {
			return fmt.Errorf("expected table %q to exist", table)
		}
	}

	for _, fn := range expectedFunctions {
		exists, err := functionExists(ctx, pool, schema, fn)
		if err != nil {
			return err
		}
		if !exists {
			return fmt.Errorf("expected function %q to exist", fn)
		}
	}

	return nil
}

func assertObjectsAbsent(ctx context.Context, pool *pgxpool.Pool, schema string) error {
	for _, table := range expectedTables {
		exists, err := tableExists(ctx, pool, schema, table)
		if err != nil {
			return err
		}
		if exists {
			return fmt.Errorf("expected table %q to be dropped by down migration", table)
		}
	}

	for _, fn := range expectedFunctions {
		exists, err := functionExists(ctx, pool, schema, fn)
		if err != nil {
			return err
		}
		if exists {
			return fmt.Errorf("expected function %q to be dropped by down migration", fn)
		}
	}

	return nil
}

func tableExists(ctx context.Context, pool *pgxpool.Pool, schema, table string) (bool, error) {
	const query = `
		SELECT EXISTS (
			SELECT 1
			FROM information_schema.tables
			WHERE table_schema = $1 AND table_name = $2
		)
	`
	var exists bool
	if err := pool.QueryRow(ctx, query, schema, table).Scan(&exists); err != nil {
		return false, err
	}
	return exists, nil
}

func functionExists(ctx context.Context, pool *pgxpool.Pool, schema, functionName string) (bool, error) {
	const query = `
		SELECT EXISTS (
			SELECT 1
			FROM pg_proc p
			JOIN pg_namespace n ON n.oid = p.pronamespace
			WHERE n.nspname = $1 AND p.proname = $2
		)
	`
	var exists bool
	if err := pool.QueryRow(ctx, query, schema, functionName).Scan(&exists); err != nil {
		return false, err
	}
	return exists, nil
}

func dropSchema(ctx context.Context, pool *pgxpool.Pool, schema string) {
	dropSQL := fmt.Sprintf("DROP SCHEMA IF EXISTS %s CASCADE", quoteIdentifier(schema))
	if _, err := pool.Exec(ctx, dropSQL); err != nil {
		log.Printf("cleanup warning: failed dropping schema %q: %v", schema, err)
	}
}

func quoteIdentifier(value string) string {
	quoted := `"`
	for _, r := range value {
		if r == '"' {
			quoted += `""`
			continue
		}
		quoted += string(r)
	}
	return quoted + `"`
}

func buildTemporarySchemaName() string {
	schemaSuffix := uuid.NewString()
	epochSeconds := time.Now().Unix()
	return fmt.Sprintf("gotchi_migration_regression_%d_%s", epochSeconds, schemaSuffix[:8])
}

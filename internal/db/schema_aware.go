package db

import (
	"context"
	"fmt"
	"strings"

	"github.com/jackc/pgx/v5/pgxpool"
)

// SchemaAwareQueries wraps Queries with schema support
// by setting search_path before executing queries
type SchemaAwareQueries struct {
	*Queries
	pool   *pgxpool.Pool
	schema string
}

// NewSchemaAwareQueries creates a Queries instance that respects schema
func NewSchemaAwareQueries(pool *pgxpool.Pool, schema string) *SchemaAwareQueries {
	return &SchemaAwareQueries{
		Queries: New(pool),
		pool:    pool,
		schema:  schema,
	}
}

// WithSchema returns a context with the schema set in search_path
func (q *SchemaAwareQueries) WithSchema(ctx context.Context) context.Context {
	if q.schema == "" {
		return ctx
	}
	// The search_path is set via the pool config in db.NewManager
	// This is a no-op for now, but could be used for per-query schema switching
	return ctx
}

// ValidateSchemaName ensures the schema name is safe to use
// Only allows alphanumeric characters and underscores
func ValidateSchemaName(schema string) error {
	if schema == "" {
		return nil
	}
	for _, r := range schema {
		if !((r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9') || r == '_') {
			return fmt.Errorf("invalid schema name %q: must contain only alphanumeric characters and underscores", schema)
		}
	}
	if len(schema) > 63 {
		return fmt.Errorf("invalid schema name %q: must be 63 characters or less", schema)
	}
	return nil
}

// QuoteIdentifier safely quotes a PostgreSQL identifier
func QuoteIdentifier(identifier string) string {
	return `"` + strings.ReplaceAll(identifier, `"`, `""`) + `"`
}

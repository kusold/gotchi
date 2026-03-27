package auth

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestValidateSchemaName_Valid(t *testing.T) {
	validSchemas := []string{
		"",             // empty is valid (uses default)
		"public",       // standard postgres schema
		"my_schema",    // underscore allowed
		"schema123",    // digits after first char
		"Schema_Name",  // mixed case
		"_private",     // starts with underscore
		"a",            // single char
		"_",            // just underscore
		strings.Repeat("a", 63), // max length (63 chars)
	}

	for _, schema := range validSchemas {
		err := validateSchemaName(schema)
		require.NoError(t, err, "schema %q should be valid", schema)
	}
}

func TestValidateSchemaName_Invalid(t *testing.T) {
	tests := []struct {
		name        string
		schema      string
		wantErrPart string
	}{
		{"starts with digit", "1schema", "must start with a letter or underscore"},
		{"contains hyphen", "my-schema", "must start with a letter or underscore"},
		{"contains dot", "my.schema", "must start with a letter or underscore"},
		{"contains special char", "schema!", "must start with a letter or underscore"},
		{"SQL injection attempt", "public; DROP TABLE", "must start with a letter or underscore"},
		{"whitespace only", "   ", "must start with a letter or underscore"},
		{"contains space", "my schema", "must start with a letter or underscore"},
		{"too long", strings.Repeat("a", 64), "must be 63 characters or less"},
		{"starts with digit underscore", "1_schema", "must start with a letter or underscore"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := validateSchemaName(tt.schema)
			require.Error(t, err, "schema %q should be rejected", tt.schema)
			assert.Contains(t, err.Error(), tt.wantErrPart)
		})
	}
}

func TestValidateSchemaName_WhitespaceOnly(t *testing.T) {
	whitespaceSchemas := []string{
		" ",    // single space
		"  ",   // multiple spaces
		"\t",   // tab
		"\n",   // newline
		" \t ", // mixed whitespace
	}

	for _, schema := range whitespaceSchemas {
		err := validateSchemaName(schema)
		require.Error(t, err, "whitespace-only schema %q should be rejected", schema)
		assert.Contains(t, err.Error(), "invalid schema name")
	}
}

func TestNewPostgresIdentityStore_NilPool(t *testing.T) {
	// Constructor should accept nil pool (validation happens at usage time)
	store, err := NewPostgresIdentityStore(nil, PostgresStoreConfig{
		Schema:            "public",
		DefaultTenantName: "Test",
	})
	require.NoError(t, err)
	require.NotNil(t, store)
}

func TestNewPostgresIdentityStore_InvalidSchema(t *testing.T) {
	// Constructor should return error for invalid schema names
	tests := []struct {
		name   string
		schema string
	}{
		{"starts with digit", "1invalid"},
		{"contains hyphen", "my-schema"},
		{"SQL injection attempt", "public; DROP TABLE"},
		{"too long", strings.Repeat("a", 64)},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := NewPostgresIdentityStore(nil, PostgresStoreConfig{
				Schema:            tt.schema,
				DefaultTenantName: "Test",
			})
			require.Error(t, err, "schema %q should be rejected", tt.schema)
			assert.Contains(t, err.Error(), "invalid schema name")
		})
	}
}

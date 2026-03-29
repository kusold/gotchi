package auth

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestNewPostgresIdentityStore_NilPool(t *testing.T) {
	// Constructor should accept nil pool (validation happens at usage time)
	store, err := NewPostgresIdentityStore(nil, PostgresStoreConfig{
		DefaultTenantName: "Test",
	})
	require.NoError(t, err)
	require.NotNil(t, store)
}

func TestNewPostgresIdentityStore_EmptyTenantName(t *testing.T) {
	// Constructor should default empty tenant name to "Default"
	store, err := NewPostgresIdentityStore(nil, PostgresStoreConfig{})
	require.NoError(t, err)
	require.NotNil(t, store)
	require.Equal(t, "Default", store.cfg.DefaultTenantName)
}

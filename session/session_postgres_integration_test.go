package session

import (
	"context"
	"flag"
	"fmt"
	"net/http"
	"os"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/kusold/gotchi/internal/testutil"
)

var testDB *testutil.TestDB

func TestMain(m *testing.M) {
	flag.Parse()
	if !testing.Short() {
		testDB = testutil.SetupTestDB(m)
		if testDB == nil {
			fmt.Println("Integration tests require a container runtime")
			os.Exit(1)
		}
	}

	code := m.Run()
	if testDB != nil {
		testDB.Close()
	}
	os.Exit(code)
}

func skipIfShort(t *testing.T) {
	t.Helper()
	if testing.Short() {
		t.Skip("skipping integration test")
	}
}

func TestNewPostgres_CreatesManager(t *testing.T) {
	skipIfShort(t)
	mgr := NewPostgres(Config{
		Lifetime:   12 * time.Hour,
		CookieName: "pg-session",
	}, testDB.Pool, "sessions")
	require.NotNil(t, mgr, "Manager should not be nil")

	inner := mgr.Inner()
	require.NotNil(t, inner)
	assert.Equal(t, 12*time.Hour, inner.Lifetime)
	assert.Equal(t, "pg-session", inner.Cookie.Name)
}

func TestNewPostgres_AppliesDefaults(t *testing.T) {
	skipIfShort(t)
	mgr := NewPostgres(Config{}, testDB.Pool, "sessions")
	require.NotNil(t, mgr)

	inner := mgr.Inner()
	assert.Equal(t, 24*time.Hour, inner.Lifetime)
	assert.Equal(t, DefaultSessionKey, inner.Cookie.Name)
	assert.Equal(t, http.SameSiteLaxMode, inner.Cookie.SameSite)
}

func TestNewPostgres_EmptyTableName(t *testing.T) {
	skipIfShort(t)
	mgr := NewPostgres(Config{}, testDB.Pool, "")
	require.NotNil(t, mgr, "Should work with empty table name (uses pgxstore default)")
}

func TestNewPostgres_SessionCRUD(t *testing.T) {
	skipIfShort(t)
	mgr := NewPostgres(Config{
		CookieName: "pg-crud-test",
	}, testDB.Pool, "sessions")

	storeHandler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mgr.Put(r.Context(), "user_id", "42")
		w.WriteHeader(http.StatusOK)
	})

	req1, rec1 := newTestRequest(t, http.MethodGet, "/")
	mgr.LoadAndSave(storeHandler).ServeHTTP(rec1, req1)

	cookies := rec1.Result().Cookies()
	require.Len(t, cookies, 1)

	var retrievedValue any
	retrieveHandler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		retrievedValue = mgr.Get(r.Context(), "user_id")
		w.WriteHeader(http.StatusOK)
	})

	req2, rec2 := newTestRequest(t, http.MethodGet, "/")
	req2.AddCookie(cookies[0])
	mgr.LoadAndSave(retrieveHandler).ServeHTTP(rec2, req2)

	assert.Equal(t, "42", retrievedValue, "Session data should persist via postgres store")
}

func TestNewPostgres_DestroySession(t *testing.T) {
	skipIfShort(t)
	mgr := NewPostgres(Config{
		CookieName: "pg-destroy-test",
	}, testDB.Pool, "sessions")

	storeHandler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mgr.Put(r.Context(), "to_delete", "value")
		w.WriteHeader(http.StatusOK)
	})

	req1, rec1 := newTestRequest(t, http.MethodGet, "/")
	mgr.LoadAndSave(storeHandler).ServeHTTP(rec1, req1)

	cookies := rec1.Result().Cookies()
	require.Len(t, cookies, 1)

	var destroyErr error
	destroyHandler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		destroyErr = mgr.Destroy(r.Context())
		w.WriteHeader(http.StatusOK)
	})

	req2, rec2 := newTestRequest(t, http.MethodGet, "/")
	req2.AddCookie(cookies[0])
	mgr.LoadAndSave(destroyHandler).ServeHTTP(rec2, req2)

	require.NoError(t, destroyErr)

	loadedCtx, err := mgr.Load(context.Background(), cookies[0].Value)
	require.NoError(t, err)
	assert.Nil(t, mgr.Get(loadedCtx, "to_delete"), "Data should be nil after destroy")
}

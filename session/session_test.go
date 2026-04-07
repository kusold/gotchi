package session

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/alexedwards/scs/v2/memstore"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestConfigWithDefaults(t *testing.T) {
	t.Parallel()

	t.Run("applies all defaults when fields are zero", func(t *testing.T) {
		t.Parallel()

		cfg := Config{}
		result := cfg.withDefaults()

		assert.Equal(t, 5*time.Minute, result.ExpiryInterval, "ExpiryInterval should default to 5 minutes")
		assert.Equal(t, 24*time.Hour, result.Lifetime, "Lifetime should default to 24 hours")
		assert.Equal(t, DefaultSessionKey, result.CookieName, "CookieName should default to 'session'")
		assert.Equal(t, http.SameSiteLaxMode, result.CookieSameSite, "CookieSameSite should default to Lax")
	})

	t.Run("preserves provided values", func(t *testing.T) {
		t.Parallel()

		cfg := Config{
			ExpiryInterval: 10 * time.Minute,
			Lifetime:       48 * time.Hour,
			CookieName:     "custom-session",
			CookieSecure:   true,
			CookieSameSite: http.SameSiteStrictMode,
		}
		result := cfg.withDefaults()

		assert.Equal(t, 10*time.Minute, result.ExpiryInterval, "ExpiryInterval should be preserved")
		assert.Equal(t, 48*time.Hour, result.Lifetime, "Lifetime should be preserved")
		assert.Equal(t, "custom-session", result.CookieName, "CookieName should be preserved")
		assert.True(t, result.CookieSecure, "CookieSecure should be preserved")
		assert.Equal(t, http.SameSiteStrictMode, result.CookieSameSite, "CookieSameSite should be preserved")
	})

	t.Run("applies defaults only for zero values", func(t *testing.T) {
		t.Parallel()

		cfg := Config{
			Lifetime:     12 * time.Hour,
			CookieSecure: true,
		}
		result := cfg.withDefaults()

		assert.Equal(t, 5*time.Minute, result.ExpiryInterval, "ExpiryInterval should default")
		assert.Equal(t, 12*time.Hour, result.Lifetime, "Lifetime should be preserved")
		assert.Equal(t, DefaultSessionKey, result.CookieName, "CookieName should default")
		assert.True(t, result.CookieSecure, "CookieSecure should be preserved")
		assert.Equal(t, http.SameSiteLaxMode, result.CookieSameSite, "CookieSameSite should default")
	})

	t.Run("does not modify original config", func(t *testing.T) {
		t.Parallel()

		cfg := Config{}
		result := cfg.withDefaults()

		assert.Equal(t, time.Duration(0), cfg.ExpiryInterval, "original should be unmodified")
		assert.Equal(t, time.Duration(0), cfg.Lifetime, "original should be unmodified")
		assert.Equal(t, "", cfg.CookieName, "original should be unmodified")
		assert.Equal(t, http.SameSite(0), cfg.CookieSameSite, "original should be unmodified")

		_ = result
	})
}

func TestRegisterGobTypes(t *testing.T) {
	t.Parallel()

	t.Run("registers custom type", func(t *testing.T) {
		type customStruct struct {
			Name string
		}

		// This should not panic
		RegisterGobTypes(customStruct{})
	})

	t.Run("registers multiple types", func(t *testing.T) {
		type type1 struct{ A int }
		type type2 struct{ B string }
		type type3 struct{ C bool }

		// This should not panic
		RegisterGobTypes(type1{}, type2{}, type3{})
	})
}

func TestNew(t *testing.T) {
	t.Parallel()

	t.Run("creates manager with custom store", func(t *testing.T) {
		t.Parallel()

		cfg := Config{
			Lifetime:       6 * time.Hour,
			CookieName:     "test-session",
			CookieSecure:   true,
			CookieSameSite: http.SameSiteStrictMode,
		}
		store := memstore.New()

		mgr := New(cfg, store)
		require.NotNil(t, mgr, "Manager should not be nil")

		inner := mgr.Inner()
		require.NotNil(t, inner, "Inner session manager should not be nil")
		assert.Equal(t, 6*time.Hour, inner.Lifetime, "Lifetime should be configured")
		assert.Equal(t, "test-session", inner.Cookie.Name, "Cookie name should be configured")
		assert.True(t, inner.Cookie.Secure, "Cookie secure should be configured")
		assert.Equal(t, http.SameSiteStrictMode, inner.Cookie.SameSite, "Cookie SameSite should be configured")
	})

	t.Run("applies defaults to config", func(t *testing.T) {
		t.Parallel()

		cfg := Config{} // All zeros
		store := memstore.New()

		mgr := New(cfg, store)
		require.NotNil(t, mgr)

		inner := mgr.Inner()
		assert.Equal(t, 24*time.Hour, inner.Lifetime, "Lifetime should have default")
		assert.Equal(t, DefaultSessionKey, inner.Cookie.Name, "CookieName should have default")
		assert.Equal(t, http.SameSiteLaxMode, inner.Cookie.SameSite, "CookieSameSite should have default")
	})
}

func TestNewMemory(t *testing.T) {
	t.Parallel()

	t.Run("creates manager with memory store", func(t *testing.T) {
		t.Parallel()

		cfg := Config{
			Lifetime:   12 * time.Hour,
			CookieName: "memory-session",
		}

		mgr := NewMemory(cfg)
		require.NotNil(t, mgr, "Manager should not be nil")

		inner := mgr.Inner()
		require.NotNil(t, inner, "Inner session manager should not be nil")
		assert.Equal(t, 12*time.Hour, inner.Lifetime, "Lifetime should be configured")
		assert.Equal(t, "memory-session", inner.Cookie.Name, "Cookie name should be configured")
	})

	t.Run("applies defaults", func(t *testing.T) {
		t.Parallel()

		cfg := Config{}
		mgr := NewMemory(cfg)
		require.NotNil(t, mgr)

		inner := mgr.Inner()
		assert.Equal(t, 24*time.Hour, inner.Lifetime, "Lifetime should have default")
		assert.Equal(t, DefaultSessionKey, inner.Cookie.Name, "CookieName should have default")
	})
}

func TestManagerLoadAndSave(t *testing.T) {
	t.Parallel()

	t.Run("middleware persists session data across requests", func(t *testing.T) {
		t.Parallel()

		mgr := NewMemory(Config{})

		// Handler that stores a value in session
		storeHandler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			mgr.Put(r.Context(), "user_id", "12345")
			w.WriteHeader(http.StatusOK)
		})

		// Handler that retrieves the value from session
		var retrievedValue any
		retrieveHandler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			retrievedValue = mgr.Get(r.Context(), "user_id")
			w.WriteHeader(http.StatusOK)
		})

		// First request: store value
		req1 := httptest.NewRequest(http.MethodGet, "/", nil)
		rec1 := httptest.NewRecorder()
		mgr.LoadAndSave(storeHandler).ServeHTTP(rec1, req1)

		// Get the session cookie from first response
		cookies := rec1.Result().Cookies()
		require.Len(t, cookies, 1, "Should have one session cookie")

		// Second request: retrieve value using the session cookie
		req2 := httptest.NewRequest(http.MethodGet, "/", nil)
		req2.AddCookie(cookies[0])
		rec2 := httptest.NewRecorder()
		mgr.LoadAndSave(retrieveHandler).ServeHTTP(rec2, req2)

		assert.Equal(t, "12345", retrievedValue, "Session data should persist across requests")
	})

	t.Run("wraps handler correctly", func(t *testing.T) {
		t.Parallel()

		mgr := NewMemory(Config{})
		handlerCalled := false

		handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			handlerCalled = true
			w.WriteHeader(http.StatusOK)
		})

		req := httptest.NewRequest(http.MethodGet, "/", nil)
		rec := httptest.NewRecorder()
		mgr.LoadAndSave(handler).ServeHTTP(rec, req)

		assert.True(t, handlerCalled, "Handler should be called")
		assert.Equal(t, http.StatusOK, rec.Code, "Status should be OK")
	})
}

func TestManagerGetAndPut(t *testing.T) {
	t.Parallel()

	t.Run("Put and Get work with context", func(t *testing.T) {
		t.Parallel()

		mgr := NewMemory(Config{})

		// Use LoadAndSave middleware to set up context
		handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			ctx := r.Context()

			// Put a value
			mgr.Put(ctx, "key1", "value1")

			// Get the value
			val := mgr.Get(ctx, "key1")
			assert.Equal(t, "value1", val)

			w.WriteHeader(http.StatusOK)
		})

		req := httptest.NewRequest(http.MethodGet, "/", nil)
		rec := httptest.NewRecorder()
		mgr.LoadAndSave(handler).ServeHTTP(rec, req)
	})

	t.Run("GetString returns string value", func(t *testing.T) {
		t.Parallel()

		mgr := NewMemory(Config{})

		handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			ctx := r.Context()

			mgr.Put(ctx, "username", "john_doe")
			val := mgr.GetString(ctx, "username")
			assert.Equal(t, "john_doe", val)

			w.WriteHeader(http.StatusOK)
		})

		req := httptest.NewRequest(http.MethodGet, "/", nil)
		rec := httptest.NewRecorder()
		mgr.LoadAndSave(handler).ServeHTTP(rec, req)
	})

	t.Run("GetString returns empty string for non-existent key", func(t *testing.T) {
		t.Parallel()

		mgr := NewMemory(Config{})

		handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			ctx := r.Context()

			val := mgr.GetString(ctx, "nonexistent")
			assert.Equal(t, "", val)

			w.WriteHeader(http.StatusOK)
		})

		req := httptest.NewRequest(http.MethodGet, "/", nil)
		rec := httptest.NewRecorder()
		mgr.LoadAndSave(handler).ServeHTTP(rec, req)
	})

	t.Run("Get returns nil for non-existent key", func(t *testing.T) {
		t.Parallel()

		mgr := NewMemory(Config{})

		handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			ctx := r.Context()

			val := mgr.Get(ctx, "nonexistent")
			assert.Nil(t, val)

			w.WriteHeader(http.StatusOK)
		})

		req := httptest.NewRequest(http.MethodGet, "/", nil)
		rec := httptest.NewRecorder()
		mgr.LoadAndSave(handler).ServeHTTP(rec, req)
	})

	t.Run("Put overwrites existing value", func(t *testing.T) {
		t.Parallel()

		mgr := NewMemory(Config{})

		handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			ctx := r.Context()

			mgr.Put(ctx, "counter", 1)
			mgr.Put(ctx, "counter", 2)

			val := mgr.Get(ctx, "counter")
			assert.Equal(t, 2, val)

			w.WriteHeader(http.StatusOK)
		})

		req := httptest.NewRequest(http.MethodGet, "/", nil)
		rec := httptest.NewRecorder()
		mgr.LoadAndSave(handler).ServeHTTP(rec, req)
	})
}

func TestManagerPutHTTP(t *testing.T) {
	t.Parallel()

	t.Run("PutHTTP uses request context", func(t *testing.T) {
		t.Parallel()

		mgr := NewMemory(Config{})

		handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			// PutHTTP uses r.Context() internally
			mgr.PutHTTP(r, "data", "test_value")

			// Verify it's accessible via Get with context
			val := mgr.Get(r.Context(), "data")
			assert.Equal(t, "test_value", val)

			w.WriteHeader(http.StatusOK)
		})

		req := httptest.NewRequest(http.MethodGet, "/", nil)
		rec := httptest.NewRecorder()
		mgr.LoadAndSave(handler).ServeHTTP(rec, req)
	})
}

func TestManagerLoad(t *testing.T) {
	t.Parallel()

	t.Run("Load creates context from token", func(t *testing.T) {
		t.Parallel()

		mgr := NewMemory(Config{})

		// First, create a session - token will be in the response cookie
		storeHandler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			mgr.Put(r.Context(), "test_key", "test_value")
			w.WriteHeader(http.StatusOK)
		})

		req1 := httptest.NewRequest(http.MethodGet, "/", nil)
		rec1 := httptest.NewRecorder()
		mgr.LoadAndSave(storeHandler).ServeHTTP(rec1, req1)

		// Get the session token from the cookie
		cookies := rec1.Result().Cookies()
		require.Len(t, cookies, 1, "Should have one session cookie")
		sessionToken := cookies[0].Value
		require.NotEmpty(t, sessionToken, "Session token should be generated")

		// Now test Load with the token directly
		ctx := context.Background()
		loadedCtx, err := mgr.Load(ctx, sessionToken)
		require.NoError(t, err, "Load should not error")
		require.NotNil(t, loadedCtx, "Loaded context should not be nil")

		// Verify we can get the value from the loaded context
		val := mgr.Get(loadedCtx, "test_key")
		assert.Equal(t, "test_value", val, "Value should be accessible from loaded context")
	})
}

func TestManagerDestroy(t *testing.T) {
	t.Parallel()

	t.Run("Destroy removes session data", func(t *testing.T) {
		t.Parallel()

		mgr := NewMemory(Config{})

		var sessionToken string
		var destroyErr error

		handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			ctx := r.Context()

			// Store a value
			mgr.Put(ctx, "to_delete", "value")

			// Get token before destroy
			sessionToken = mgr.Inner().Token(ctx)

			// Destroy the session
			destroyErr = mgr.Destroy(ctx)

			w.WriteHeader(http.StatusOK)
		})

		req := httptest.NewRequest(http.MethodGet, "/", nil)
		rec := httptest.NewRecorder()
		mgr.LoadAndSave(handler).ServeHTTP(rec, req)

		assert.NoError(t, destroyErr, "Destroy should not error")

		// Load the destroyed session - data should be gone
		ctx := context.Background()
		loadedCtx, err := mgr.Load(ctx, sessionToken)
		require.NoError(t, err)

		val := mgr.Get(loadedCtx, "to_delete")
		assert.Nil(t, val, "Data should be nil after destroy")
	})
}

func TestManagerInner(t *testing.T) {
	t.Parallel()

	t.Run("returns underlying session manager", func(t *testing.T) {
		t.Parallel()

		mgr := NewMemory(Config{
			Lifetime:   2 * time.Hour,
			CookieName: "inner-test",
		})

		inner := mgr.Inner()
		require.NotNil(t, inner, "Inner should not return nil")
		assert.Equal(t, 2*time.Hour, inner.Lifetime, "Inner should have configured lifetime")
		assert.Equal(t, "inner-test", inner.Cookie.Name, "Inner should have configured cookie name")
	})

	t.Run("returns same instance on multiple calls", func(t *testing.T) {
		t.Parallel()

		mgr := NewMemory(Config{})

		inner1 := mgr.Inner()
		inner2 := mgr.Inner()

		assert.Same(t, inner1, inner2, "Inner should return same instance")
	})
}

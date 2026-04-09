// Package session provides HTTP session management for the gotchi framework,
// wrapping the SCS (Secure Cookie Sessions) library with a simplified API. It
// supports both in-memory and PostgreSQL-backed session stores, with sensible
// defaults for cookie configuration and session lifetime.
//
// The package centers on the [Manager] type, which wraps an SCS SessionManager
// and exposes methods for loading, saving, reading, and writing session data.
// Managers are created via the [NewMemory] or [NewPostgres] constructor
// functions, or the generic [New] function when a custom [scs.Store] is needed.
//
// # Quick Start
//
// Create an in-memory session manager and use it as HTTP middleware:
//
//	mgr := session.NewMemory(session.Config{
//	    Lifetime:   12 * time.Hour,
//	    CookieName: "myapp-session",
//	})
//
//	// Wrap your handler with session middleware.
//	handler := mgr.LoadAndSave(myHandler)
//	http.ListenAndServe(":8080", handler)
//
// For production use with PostgreSQL:
//
//	pool, _ := pgxpool.New(ctx, databaseURL)
//	mgr := session.NewPostgres(session.Config{}, pool, "sessions")
//
// # Storing and Retrieving Values
//
// Within a handler wrapped by [Manager.LoadAndSave], use the context to
// interact with session data:
//
//	func myHandler(w http.ResponseWriter, r *http.Request) {
//	    mgr.Put(r.Context(), "user_id", 42)
//	    id := mgr.GetString(r.Context(), "user_id")
//	}
//
// # Custom Types
//
// To store custom types in sessions, register them with gob before creating
// the manager:
//
//	type UserSession struct{ Name string; Role string }
//	session.RegisterGobTypes(UserSession{})
//
//	mgr := session.NewMemory(session.Config{})
package session

import (
	"context"
	"encoding/gob"
	"net/http"
	"time"

	"github.com/alexedwards/scs/pgxstore"
	"github.com/alexedwards/scs/v2"
	"github.com/alexedwards/scs/v2/memstore"
	"github.com/jackc/pgx/v5/pgxpool"
)

// DefaultSessionKey is the default cookie name used when Config.CookieName is
// not explicitly set.
const DefaultSessionKey = "session"

// Config holds the configuration options for a session Manager. Zero-value
// fields are replaced with sensible defaults when passed to any constructor.
//
// Defaults applied by the package:
//   - ExpiryInterval: 5 minutes
//   - Lifetime:       24 hours
//   - CookieName:     "session" (see [DefaultSessionKey])
//   - CookieSecure:   false
//   - CookieSameSite: [http.SameSiteLaxMode]
type Config struct {
	// ExpiryInterval controls how often the session store runs cleanup of
	// expired sessions. Defaults to 5 minutes if zero or negative.
	ExpiryInterval time.Duration

	// Lifetime is the maximum duration a session remains valid. Defaults to
	// 24 hours if zero or negative.
	Lifetime time.Duration

	// CookieName is the name of the HTTP cookie that holds the session token.
	// Defaults to "session" if empty.
	CookieName string

	// CookieSecure sets the Secure attribute on the session cookie. When true,
	// the cookie is only sent over HTTPS connections. Defaults to false.
	CookieSecure bool

	// CookieSameSite sets the SameSite attribute on the session cookie.
	// Defaults to [http.SameSiteLaxMode] if zero.
	CookieSameSite http.SameSite
}

// withDefaults returns a copy of the Config with zero-value fields replaced by
// their default values. The original Config is not modified.
func (c Config) withDefaults() Config {
	cfg := c
	if cfg.ExpiryInterval <= 0 {
		cfg.ExpiryInterval = 5 * time.Minute
	}
	if cfg.Lifetime <= 0 {
		cfg.Lifetime = 24 * time.Hour
	}
	if cfg.CookieName == "" {
		cfg.CookieName = DefaultSessionKey
	}
	if cfg.CookieSameSite == 0 {
		cfg.CookieSameSite = http.SameSiteLaxMode
	}
	return cfg
}

// Manager wraps an SCS SessionManager and provides a simplified API for HTTP
// session management. Use one of the constructor functions ([New], [NewMemory],
// or [NewPostgres]) to create a Manager instance.
type Manager struct {
	sessionManager *scs.SessionManager
}

// RegisterGobTypes registers one or more custom types with the gob encoding
// package. This is required before storing non-primitive types (structs,
// maps, slices of custom types, etc.) in session data. Call this function
// before creating a Manager and before any session read/write operations.
//
// For example, to register a custom struct for session storage:
//
//	type CartItem struct {
//	    ProductID int
//	    Quantity  int
//	}
//	session.RegisterGobTypes(CartItem{})
func RegisterGobTypes(values ...any) {
	for _, value := range values {
		gob.Register(value)
	}
}

// New creates a new Manager with the provided Config and a custom SCS Store
// implementation. This is the most flexible constructor, allowing use of any
// store that implements the [scs.Store] interface.
//
// Zero-value fields in cfg are replaced with defaults (see [Config]).
func New(cfg Config, store scs.Store) *Manager {
	c := cfg.withDefaults()
	sm := scs.New()
	sm.Lifetime = c.Lifetime
	sm.Cookie.Name = c.CookieName
	sm.Cookie.Secure = c.CookieSecure
	sm.Cookie.SameSite = c.CookieSameSite
	sm.Store = store

	return &Manager{sessionManager: sm}
}

// NewMemory creates a new Manager backed by an in-memory store. Sessions are
// held in process memory and are lost when the process restarts. This
// constructor is suitable for development and testing, or for applications
// where session persistence across restarts is not required.
//
// Zero-value fields in cfg are replaced with defaults (see [Config]).
func NewMemory(cfg Config) *Manager {
	c := cfg.withDefaults()
	store := memstore.NewWithCleanupInterval(c.ExpiryInterval)
	return New(c, store)
}

// NewPostgres creates a new Manager backed by a PostgreSQL store using the
// provided connection pool. Sessions persist across process restarts, making
// this suitable for production deployments.
//
// The tableName parameter specifies the database table used for session
// storage. If tableName is empty, the pgxstore default table name is used.
//
// Zero-value fields in cfg are replaced with defaults (see [Config]).
func NewPostgres(cfg Config, pool *pgxpool.Pool, tableName string) *Manager {
	c := cfg.withDefaults()
	storeCfg := pgxstore.Config{CleanUpInterval: c.ExpiryInterval}
	if tableName != "" {
		storeCfg.TableName = tableName
	}
	store := pgxstore.NewWithConfig(pool, storeCfg)
	return New(c, store)
}

// LoadAndSave returns an HTTP handler that automatically loads session data
// from the request cookie before passing it to next, and saves any modified
// session data back to the response cookie afterward. This is the primary
// middleware for enabling sessions in your HTTP handler chain.
//
// Typically used at the top of your middleware stack:
//
//	r := chi.NewRouter()
//	r.Use(mgr.LoadAndSave)
func (m *Manager) LoadAndSave(next http.Handler) http.Handler {
	return m.sessionManager.LoadAndSave(next)
}

// Load retrieves session data associated with the given token and returns a
// new context.Context carrying the session state. This is useful for loading
// sessions outside of the standard middleware flow, such as in WebSocket
// upgrade handlers or background workers that receive a session token.
func (m *Manager) Load(ctx context.Context, token string) (context.Context, error) {
	return m.sessionManager.Load(ctx, token)
}

// Get retrieves the value associated with key from the session in ctx. The
// return type is any; the caller must type-assert the result. Returns nil if
// the key does not exist in the session.
func (m *Manager) Get(ctx context.Context, key string) any {
	return m.sessionManager.Get(ctx, key)
}

// GetString retrieves the value associated with key from the session in ctx,
// type-asserted to a string. Returns an empty string if the key does not exist
// or the value is not a string.
func (m *Manager) GetString(ctx context.Context, key string) string {
	return m.sessionManager.GetString(ctx, key)
}

// Put stores a key-value pair in the session associated with ctx. The value
// must be a type registered with gob if it is not a built-in type. See
// [RegisterGobTypes] for registering custom types.
func (m *Manager) Put(ctx context.Context, key string, value any) {
	m.sessionManager.Put(ctx, key, value)
}

// PutHTTP stores a key-value pair in the session associated with the given
// HTTP request's context. This is a convenience wrapper around [Manager.Put]
// that extracts the context from the request, allowing a shorter call in HTTP
// handlers:
//
//	mgr.PutHTTP(r, "flash", "Item saved")
func (m *Manager) PutHTTP(r *http.Request, key string, value any) {
	m.sessionManager.Put(r.Context(), key, value)
}

// Destroy deletes all session data associated with the context and expires the
// session cookie. Use this to implement logout functionality. Returns an error
// if the underlying store fails to remove the session data.
func (m *Manager) Destroy(ctx context.Context) error {
	return m.sessionManager.Destroy(ctx)
}

// Inner returns the underlying [scs.SessionManager] instance. Use this to
// access advanced SCS features not exposed by the Manager wrapper, such as
// token inspection, iteration over session keys, or custom cookie settings.
func (m *Manager) Inner() *scs.SessionManager {
	return m.sessionManager
}

package app

import (
	"errors"
	"fmt"
	"log/slog"
	"net/http"

	"github.com/go-chi/cors"
	"github.com/kusold/gotchi/auth"
	"github.com/kusold/gotchi/db"
	"github.com/kusold/gotchi/observability"
	"github.com/kusold/gotchi/openapi"
	"github.com/kusold/gotchi/session"
)

// Option configures an Application during construction.
type Option func(*builder) error

// CORSConfig controls cross-origin resource sharing behavior.
// For simple cases, use WithCORS which applies sensible defaults.
// For full control, use WithCORSConfig.
type CORSConfig struct {
	cors.Options
}

// corsDefaults returns a CORSConfig with the opinionated defaults
// that were previously hardcoded.
func corsDefaults(first string, rest ...string) CORSConfig {
	origins := append([]string{first}, rest...)
	return CORSConfig{
		Options: cors.Options{
			AllowedOrigins:   origins,
			AllowedMethods:   []string{"GET", "POST", "PUT", "PATCH", "DELETE", "OPTIONS"},
			AllowedHeaders:   []string{"Accept", "Authorization", "Content-Type", "X-CSRF-Token", "X-Request-ID"},
			ExposedHeaders:   []string{"Link", "X-Request-ID"},
			AllowCredentials: true,
			MaxAge:           300,
		},
	}
}

// builder accumulates configuration from options before constructing the Application.
type builder struct {
	// server
	port string

	// database (required)
	dbConfig *db.Config

	// sessions
	sessionConfig *session.Config

	// auth (optional, auto-enables sessions)
	authConfig    *auth.Config
	identityStore auth.IdentityStore
	loginHandler  http.HandlerFunc

	// observability (optional)
	otelConfig *observability.OTELConfig

	// CORS (optional)
	corsConfig *CORSConfig

	// OpenAPI
	openAPIConfig *openapi.Config

	// migrations
	migrationSources     []db.MigrationSource
	enableCoreMigrations bool
	enableAuthMigrations bool

	// middleware
	middleware               []func(http.Handler) http.Handler
	disableDefaultMiddleware bool

	// modules
	modules []Module

	// testing/advanced
	clock  Clock
	logger *slog.Logger
}

func (b *builder) validate() error {
	if b.dbConfig == nil {
		return fmt.Errorf("database is required: use WithDatabase or WithDatabaseConfig")
	}
	if b.dbConfig.DatabaseURL == "" {
		return fmt.Errorf("database URL is required")
	}
	if b.authConfig != nil {
		if b.authConfig.IssuerURL == "" || b.authConfig.ClientID == "" || b.authConfig.ClientSecret == "" || b.authConfig.RedirectURL == "" {
			return fmt.Errorf("OIDC issuer/client credentials/redirect URL are required when auth is enabled")
		}
	}
	return nil
}

func (b *builder) applyDefaults() {
	if b.port == "" {
		b.port = "3000"
	}
	if b.authConfig != nil {
		defaults := b.authConfig.WithDefaults()
		b.authConfig = &defaults
		// Auto-enable sessions when auth is configured
		if b.sessionConfig == nil {
			b.sessionConfig = &session.Config{}
		}
	}
	if b.logger == nil {
		b.logger = slog.Default()
	}
	if b.clock == nil {
		b.clock = realClock{}
	}
}

// WithDatabase configures the database connection with a URL.
// This is required.
func WithDatabase(url string) Option {
	return func(b *builder) error {
		b.dbConfig = &db.Config{DatabaseURL: url}
		return nil
	}
}

// WithDatabaseConfig configures the database with full control over all options
// including tracing and search path. This is an alternative to WithDatabase.
func WithDatabaseConfig(cfg db.Config) Option {
	return func(b *builder) error {
		b.dbConfig = &cfg
		return nil
	}
}

// WithPort sets the HTTP server port. Defaults to "3000".
func WithPort(port string) Option {
	return func(b *builder) error {
		b.port = port
		return nil
	}
}

// WithAuth enables OIDC authentication with the provided configuration.
// The Enabled field on the config is ignored — calling WithAuth is the
// signal that auth should be enabled. Sessions are automatically enabled
// with default settings if not explicitly configured via WithSessions.
func WithAuth(cfg auth.Config) Option {
	return func(b *builder) error {
		b.authConfig = &cfg
		return nil
	}
}

// WithIdentityStore overrides the default Postgres identity store.
// Only takes effect when auth is enabled via WithAuth.
func WithIdentityStore(store auth.IdentityStore) Option {
	return func(b *builder) error {
		b.identityStore = store
		return nil
	}
}

// WithLoginHandler overrides the default login page handler.
// Only takes effect when auth is enabled via WithAuth.
func WithLoginHandler(h http.HandlerFunc) Option {
	return func(b *builder) error {
		b.loginHandler = h
		return nil
	}
}

// WithSessions explicitly configures session management.
// Sessions are also auto-enabled when WithAuth is used.
func WithSessions(cfg session.Config) Option {
	return func(b *builder) error {
		b.sessionConfig = &cfg
		return nil
	}
}

// WithOTEL enables OpenTelemetry with the provided configuration.
// The Enabled field on the config is ignored — calling WithOTEL is the
// signal that OTEL should be enabled.
func WithOTEL(cfg observability.OTELConfig) Option {
	return func(b *builder) error {
		b.otelConfig = &cfg
		return nil
	}
}

// WithCORS enables CORS with the specified allowed origins and
// sensible defaults for methods, headers, credentials, and max age.
// For full control over all CORS options, use WithCORSConfig instead.
//
// Wildcard origins ("*") are not allowed because this function always
// sets AllowCredentials to true, which is incompatible with "*" per the
// CORS spec. Use WithCORSConfig with AllowCredentials: false if you need
// a wildcard origin.
func WithCORS(first string, rest ...string) Option {
	return func(b *builder) error {
		cfg := corsDefaults(first, rest...)
		if err := cfg.validate(); err != nil {
			return err
		}
		b.corsConfig = &cfg
		return nil
	}
}

// WithCORSConfig enables CORS with full control over all options.
// Returns an error if the configuration is invalid (e.g., wildcard origin
// with credentials enabled, or empty origins).
func WithCORSConfig(cfg CORSConfig) Option {
	return func(b *builder) error {
		if err := cfg.validate(); err != nil {
			return err
		}
		b.corsConfig = &cfg
		return nil
	}
}

// validate checks the CORSConfig for common misconfigurations.
func (c CORSConfig) validate() error {
	if len(c.AllowedOrigins) == 0 {
		return errors.New("CORS requires at least one allowed origin")
	}
	for _, o := range c.AllowedOrigins {
		if o == "*" && c.AllowCredentials {
			return errors.New("CORS wildcard origin (*) is incompatible with AllowCredentials: true; use specific origins or set AllowCredentials to false")
		}
	}
	return nil
}

// WithOpenAPI configures OpenAPI request/response validation.
func WithOpenAPI(cfg openapi.Config) Option {
	return func(b *builder) error {
		b.openAPIConfig = &cfg
		return nil
	}
}

// WithCoreMigrations enables the built-in core schema migrations
// (sessions table, set_tenant function).
func WithCoreMigrations() Option {
	return func(b *builder) error {
		b.enableCoreMigrations = true
		return nil
	}
}

// WithAuthMigrations enables the built-in auth schema migrations
// (tenants, users, tenant_memberships tables).
func WithAuthMigrations() Option {
	return func(b *builder) error {
		b.enableAuthMigrations = true
		return nil
	}
}

// WithMigrations adds custom migration sources.
func WithMigrations(sources ...db.MigrationSource) Option {
	return func(b *builder) error {
		b.migrationSources = append(b.migrationSources, sources...)
		return nil
	}
}

// WithMiddleware appends custom middleware to the chain.
// Middleware is applied after default middleware (if enabled) and
// feature-specific middleware (CORS, OTEL, sessions).
func WithMiddleware(mw ...func(http.Handler) http.Handler) Option {
	return func(b *builder) error {
		b.middleware = append(b.middleware, mw...)
		return nil
	}
}

// WithNoDefaultMiddleware disables the default middleware stack
// (RealIP, Logger, Recoverer). Use this for full control over
// the middleware chain via WithMiddleware.
func WithNoDefaultMiddleware() Option {
	return func(b *builder) error {
		b.disableDefaultMiddleware = true
		return nil
	}
}

// WithModule registers application modules that define routes
// and consume shared dependencies.
func WithModule(modules ...Module) Option {
	return func(b *builder) error {
		b.modules = append(b.modules, modules...)
		return nil
	}
}

// WithClock overrides the default clock. Useful for testing.
func WithClock(c Clock) Option {
	return func(b *builder) error {
		b.clock = c
		return nil
	}
}

// WithLogger overrides the default logger.
func WithLogger(l *slog.Logger) Option {
	return func(b *builder) error {
		b.logger = l
		return nil
	}
}

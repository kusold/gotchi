// Package app provides a multi-tenant web application framework built on top of
// the Chi router. It orchestrates database connections, schema migrations,
// session management, OpenID Connect authentication, OpenAPI specification
// serving, and OpenTelemetry observability into a single cohesive Application
// type.
//
// The central type is [Application], which is created with [New] using the
// functional options pattern and started with [Application.Run]. An Application
// is composed of zero or more [Module] implementations that register routes on
// the Chi router and receive shared [Dependencies] (database pool, session
// manager, auth handler, etc.).
//
// # Quick Start
//
// Create a minimal application with a database URL and a custom module:
//
//	application, err := app.New(
//	    app.WithDatabase("postgres://user:pass@localhost/mydb"),
//	    app.WithCoreMigrations(),
//	    app.WithMigrations(db.MigrationSource{FS: myMigrationFS, Dir: "migrations"}),
//	    app.WithModule(app.ModuleFunc(func(r chi.Router, deps app.Dependencies) error {
//	        r.Get("/health", func(w http.ResponseWriter, _ *http.Request) {
//	            w.WriteHeader(http.StatusOK)
//	        })
//	        return nil
//	    })),
//	)
//	if err != nil {
//	    log.Fatal(err)
//	}
//
//	if err := application.Run(context.Background()); err != nil {
//	    log.Fatal(err)
//	}
//
// # Options
//
// Applications are configured using [Option] functions passed to [New].
// Required options include [WithDatabase] (or [WithDatabaseConfig]). All other
// options are optional and have sensible defaults:
//
//   - [WithPort] — HTTP server port (default "3000")
//   - [WithAuth] — enables OIDC authentication (auto-enables sessions)
//   - [WithSessions] — explicit session configuration
//   - [WithOTEL] — OpenTelemetry tracing and metrics
//   - [WithCORS] / [WithCORSConfig] — cross-origin resource sharing
//   - [WithCoreMigrations] / [WithAuthMigrations] / [WithMigrations] — database schemas
//   - [WithMiddleware] / [WithNoDefaultMiddleware] — HTTP middleware chain
//   - [WithModule] — registers application modules
//   - [WithClock] / [WithLogger] — testing and observability overrides
//
// # Modules
//
// A [Module] is any type that implements the single-method interface:
//
//	type Module interface {
//	    Register(r chi.Router, deps Dependencies) error
//	}
//
// For convenience, [ModuleFunc] wraps a plain function so it satisfies the
// [Module] interface, similar to http.HandlerFunc.
//
// # Validation and Defaults
//
// Options are validated during [New]. The builder applies defaults for
// unspecified optional fields (e.g., port defaults to "3000", logger defaults to
// slog.Default). See [builder.applyDefaults] for the full list of defaults.
package app

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"
	chiMiddleware "github.com/go-chi/chi/v5/middleware"
	"github.com/go-chi/cors"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/kusold/gotchi/auth"
	"github.com/kusold/gotchi/auth/password"
	"github.com/kusold/gotchi/db"
	"github.com/kusold/gotchi/migrations"
	"github.com/kusold/gotchi/observability"
	"github.com/kusold/gotchi/openapi"
	"github.com/kusold/gotchi/session"
)

// Clock abstracts time for testability.
type Clock interface {
	// Now returns the current time according to the clock implementation.
	Now() time.Time
}

// realClock is the production Clock implementation that delegates to time.Now.
type realClock struct{}

const defaultOTELShutdownTimeout = 5 * time.Second

// Now returns the current local time.
func (realClock) Now() time.Time {
	return time.Now()
}

// Module defines a self-contained unit of routes and behavior
// that is registered with shared dependencies.
type Module interface {
	// Register wires the module into the Chi router using the provided
	// Dependencies. It is called once during [Application.Run].
	Register(r chi.Router, deps Dependencies) error
}

// ModuleFunc adapts an ordinary function to the Module interface.
type ModuleFunc func(r chi.Router, deps Dependencies) error

// Register calls f(r, deps), satisfying the [Module] interface.
func (f ModuleFunc) Register(r chi.Router, deps Dependencies) error {
	return f(r, deps)
}

// Dependencies holds the initialized services available to modules.
type Dependencies struct {
	// DB is the database manager responsible for connection pooling and
	// running migrations.
	DB *db.Manager

	// Pool is the underlying pgxpool connection pool. Use this for direct
	// database queries within modules.
	Pool *pgxpool.Pool

	// Session provides cookie- or header-based session management backed by
	// PostgreSQL.
	Session *session.Manager

	// Auth is the OpenID Connect handler, or nil when OIDC is disabled in
	// the configuration.
	Auth *auth.OIDCHandler

	// Password is the password authentication handler, or nil when password
	// auth is disabled in the configuration.
	Password *password.PasswordHandler

	// IdentityStore provides user identity lookup and persistence. It is
	// non-nil only when OIDC authentication is enabled.
	IdentityStore auth.IdentityStore

	// OpenAPI holds the configuration for serving OpenAPI documentation
	// endpoints.
	OpenAPI openapi.Config

	// Logger is the application-wide structured logger (slog).
	Logger *slog.Logger

	// Clock provides the current time and can be replaced in tests.
	Clock Clock
}

// Application is the main entry point. Construct one with New and
// start it with Run.
type Application struct {
	config builder

	// runtime state
	router       *chi.Mux
	db           *db.Manager
	dependencies Dependencies
	resolvedOTEL *observability.OTELConfig
	otelShutdown func(context.Context) error
}

// New creates an Application from the supplied options.
// At minimum, WithDatabase or WithDatabaseConfig must be provided.
func New(opts ...Option) (*Application, error) {
	b := &builder{}

	for _, opt := range opts {
		if err := opt(b); err != nil {
			return nil, err
		}
	}

	b.applyDefaults()

	if err := b.validate(); err != nil {
		return nil, err
	}

	database := db.NewManager(*b.dbConfig)

	a := &Application{
		config: *b,
		router: chi.NewRouter(),
		db:     database,
	}

	return a, nil
}

// Router returns the underlying chi router.
func (a *Application) Router() chi.Router {
	return a.router
}

// Dependencies returns the initialized service dependencies.
// Dependencies are only fully populated after Run begins.
func (a *Application) Dependencies() Dependencies {
	return a.dependencies
}

// Run initializes all configured subsystems and starts the HTTP server.
// It blocks until the server exits or ctx is cancelled.
func (a *Application) Run(ctx context.Context) error {
	cfg := &a.config

	// --- OTEL ---
	if cfg.otelConfig != nil {
		resolved := cfg.otelConfig.WithDefaults()
		resolved.Enabled = true
		a.resolvedOTEL = &resolved
		shutdown, err := observability.SetupOTEL(ctx, resolved)
		if err != nil {
			return fmt.Errorf("setting up OTEL: %w", err)
		}
		a.otelShutdown = shutdown
		if resolved.TracingEnabled() {
			a.db.EnableOTELTracing()
		}
	}

	// --- Database ---
	if err := a.db.Connect(ctx); err != nil {
		return err
	}

	// --- Migrations ---
	if cfg.enableCoreMigrations {
		a.db.AddMigrationSource(db.MigrationSource{FS: migrations.Core(), Dir: "."})
	}
	if cfg.enableAuthMigrations {
		a.db.AddMigrationSource(db.MigrationSource{FS: migrations.Auth(), Dir: "."})
	}
	if cfg.enablePasswordMigrations {
		a.db.AddMigrationSource(db.MigrationSource{FS: migrations.Password(), Dir: "."})
	}
	for _, source := range cfg.migrationSources {
		a.db.AddMigrationSource(source)
	}
	if err := a.db.RunMigrations(ctx); err != nil {
		return err
	}

	// --- Sessions ---
	var sessionManager *session.Manager
	if cfg.sessionConfig != nil {
		sessionManager = session.NewPostgres(*cfg.sessionConfig, a.db.Pool(), "sessions")
		session.RegisterGobTypes(auth.SessionClaims{})
	}

	// --- Identity Store ---
	identityStore := cfg.identityStore
	if cfg.authConfig != nil && identityStore == nil {
		var err error
		identityStore, err = auth.NewPostgresIdentityStore(a.db.Pool(), auth.PostgresStoreConfig{})
		if err != nil {
			return fmt.Errorf("failed to create identity store: %w", err)
		}
	}

	// --- OIDC Handler ---
	var oidcHandler *auth.OIDCHandler
	if cfg.authConfig != nil {
		authCfg := *cfg.authConfig
		authCfg.Enabled = true
		handler, err := auth.NewOIDCHandler(authCfg, sessionManager, identityStore)
		if err != nil {
			return err
		}
		oidcHandler = handler
	}

	// --- Password Auth Handler ---
	var passwordHandler *password.PasswordHandler
	if cfg.passwordConfig != nil {
		if identityStore == nil {
			var err error
			identityStore, err = auth.NewPostgresIdentityStore(a.db.Pool(), auth.PostgresStoreConfig{})
			if err != nil {
				return fmt.Errorf("failed to create identity store: %w", err)
			}
		}
		// Password auth is tightly coupled to PostgresIdentityStore because it
		// needs direct access to the underlying pg pool for password-specific
		// queries. A custom identity store set via WithIdentityStore is
		// unsupported and will cause startup to fail here.
		pgStore, ok := identityStore.(*auth.PostgresIdentityStore)
		if !ok {
			return fmt.Errorf("password auth requires a PostgresIdentityStore")
		}
		pwStore, err := password.NewPasswordIdentityStore(a.db.Pool(), pgStore, *cfg.passwordConfig, cfg.logger)
		if err != nil {
			return fmt.Errorf("failed to create password identity store: %w", err)
		}
		passwordHandler = password.NewPasswordHandler(*cfg.passwordConfig, pwStore, sessionManager)
	}

	// --- Dependencies ---
	var oaCfg openapi.Config
	if cfg.openAPIConfig != nil {
		oaCfg = *cfg.openAPIConfig
	}

	a.dependencies = Dependencies{
		DB:            a.db,
		Pool:          a.db.Pool(),
		Session:       sessionManager,
		Auth:          oidcHandler,
		Password:      passwordHandler,
		IdentityStore: identityStore,
		OpenAPI:       oaCfg,
		Logger:        cfg.logger,
		Clock:         cfg.clock,
	}

	// --- Middleware ---
	a.setupMiddleware(sessionManager)

	// --- Auth routes ---
	if oidcHandler != nil {
		a.router.Route("/auth", func(r chi.Router) {
			if cfg.loginHandler != nil {
				r.Get("/login", cfg.loginHandler)
			} else {
				r.Get("/login", defaultLoginHandler)
			}
			oidcHandler.RegisterRoutes(r)
		})
	}

	// --- Password routes ---
	if passwordHandler != nil {
		a.router.Route(cfg.passwordConfig.PathPrefix, func(r chi.Router) {
			passwordHandler.RegisterRoutes(r)
		})
	}

	// --- Modules ---
	for _, module := range cfg.modules {
		if err := module.Register(a.router, a.dependencies); err != nil {
			return err
		}
	}

	return http.ListenAndServe(fmt.Sprintf(":%s", cfg.port), a.router)
}

// setupMiddleware applies all configured middleware to the router.
// It is called by Run after services are initialized. Extracted for testability.
func (a *Application) setupMiddleware(sessionManager *session.Manager) {
	cfg := &a.config

	if !cfg.disableDefaultMiddleware {
		a.router.Use(chiMiddleware.RealIP)
		a.router.Use(chiMiddleware.Logger)
		a.router.Use(chiMiddleware.Recoverer)
	}

	if cfg.corsConfig != nil {
		a.router.Use(cors.Handler(cfg.corsConfig.Options))
	}

	if a.resolvedOTEL != nil {
		if a.resolvedOTEL.TracingEnabled() {
			a.router.Use(observability.OTELTracingMiddleware(a.resolvedOTEL.ServiceName))
		}
		if a.resolvedOTEL.MetricsEnabled() {
			a.router.Use(observability.OTELMetricsMiddleware(a.resolvedOTEL.ServiceName))
		}
	}

	if sessionManager != nil {
		a.router.Use(sessionManager.LoadAndSave)
		sessionKey := auth.DefaultSessionKey
		if cfg.authConfig != nil && cfg.authConfig.SessionKey != "" {
			sessionKey = cfg.authConfig.SessionKey
		}
		a.router.Use(observability.CorrelationAndAudit(sessionManager, sessionKey))
	}

	for _, mw := range cfg.middleware {
		a.router.Use(mw)
	}
}

// Close performs graceful shutdown of the application's resources.
func (a *Application) Close() error {
	var errs []error
	if err := a.db.Close(); err != nil {
		errs = append(errs, err)
	}
	if a.otelShutdown != nil {
		timeout := defaultOTELShutdownTimeout
		if a.resolvedOTEL != nil {
			timeout = a.resolvedOTEL.ShutdownTimeout
		}
		ctx, cancel := context.WithTimeout(context.Background(), timeout)
		defer cancel()
		if err := a.otelShutdown(ctx); err != nil {
			errs = append(errs, err)
		}
	}
	return errors.Join(errs...)
}

func defaultLoginHandler(w http.ResponseWriter, _ *http.Request) {
	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write([]byte("login endpoint"))
}

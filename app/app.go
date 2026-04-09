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
	"github.com/kusold/gotchi/db"
	"github.com/kusold/gotchi/migrations"
	"github.com/kusold/gotchi/observability"
	"github.com/kusold/gotchi/openapi"
	"github.com/kusold/gotchi/session"
)

// Clock abstracts time for testability.
type Clock interface {
	Now() time.Time
}

type realClock struct{}

func (realClock) Now() time.Time {
	return time.Now()
}

// Module defines a self-contained unit of routes and behavior
// that is registered with shared dependencies.
type Module interface {
	Register(r chi.Router, deps Dependencies) error
}

// ModuleFunc adapts an ordinary function to the Module interface.
type ModuleFunc func(r chi.Router, deps Dependencies) error

func (f ModuleFunc) Register(r chi.Router, deps Dependencies) error {
	return f(r, deps)
}

// Dependencies holds the initialized services available to modules.
type Dependencies struct {
	DB            *db.Manager
	Pool          *pgxpool.Pool
	Session       *session.Manager
	Auth          *auth.OIDCHandler
	IdentityStore auth.IdentityStore
	OpenAPI       openapi.Config
	Logger        *slog.Logger
	Clock         Clock
}

// Application is the main entry point. Construct one with New and
// start it with Run.
type Application struct {
	// resolved configuration from builder
	port                     string
	dbConfig                 db.Config
	sessionConfig            *session.Config
	authConfig               *auth.Config
	identityStore            auth.IdentityStore
	loginHandler             http.HandlerFunc
	otelConfig               *observability.OTELConfig
	corsOrigins              []string
	openAPIConfig            openapi.Config
	migrationSources         []db.MigrationSource
	enableCoreMigrations     bool
	enableAuthMigrations     bool
	middleware               []func(http.Handler) http.Handler
	disableDefaultMiddleware bool
	modules                  []Module
	clock                    Clock
	logger                   *slog.Logger

	// runtime state
	router       *chi.Mux
	db           *db.Manager
	dependencies Dependencies
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

	var oaCfg openapi.Config
	if b.openAPIConfig != nil {
		oaCfg = *b.openAPIConfig
	}

	a := &Application{
		port:                     b.port,
		dbConfig:                 *b.dbConfig,
		sessionConfig:            b.sessionConfig,
		authConfig:               b.authConfig,
		identityStore:            b.identityStore,
		loginHandler:             b.loginHandler,
		otelConfig:               b.otelConfig,
		corsOrigins:              b.corsOrigins,
		openAPIConfig:            oaCfg,
		migrationSources:         b.migrationSources,
		enableCoreMigrations:     b.enableCoreMigrations,
		enableAuthMigrations:     b.enableAuthMigrations,
		middleware:               b.middleware,
		disableDefaultMiddleware: b.disableDefaultMiddleware,
		modules:                  b.modules,
		clock:                    b.clock,
		logger:                   b.logger,

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
	// --- OTEL ---
	if a.otelConfig != nil {
		shutdown, err := observability.SetupOTEL(ctx, *a.otelConfig)
		if err != nil {
			return fmt.Errorf("setting up OTEL: %w", err)
		}
		a.otelShutdown = shutdown
		if a.otelConfig.TracingEnabled() {
			a.db.EnableOTELTracing()
		}
	}

	// --- Database ---
	if err := a.db.Connect(ctx); err != nil {
		return err
	}

	// --- Migrations ---
	if a.enableCoreMigrations {
		a.db.AddMigrationSource(db.MigrationSource{FS: migrations.Core(), Dir: "."})
	}
	if a.enableAuthMigrations {
		a.db.AddMigrationSource(db.MigrationSource{FS: migrations.Auth(), Dir: "."})
	}
	for _, source := range a.migrationSources {
		a.db.AddMigrationSource(source)
	}
	if err := a.db.RunMigrations(ctx); err != nil {
		return err
	}

	// --- Sessions ---
	var sessionManager *session.Manager
	if a.sessionConfig != nil {
		sessionManager = session.NewPostgres(*a.sessionConfig, a.db.Pool(), "sessions")
		session.RegisterGobTypes(auth.SessionClaims{})
	}

	// --- Identity Store ---
	identityStore := a.identityStore
	if a.authConfig != nil && identityStore == nil {
		var err error
		identityStore, err = auth.NewPostgresIdentityStore(a.db.Pool(), auth.PostgresStoreConfig{})
		if err != nil {
			return fmt.Errorf("failed to create identity store: %w", err)
		}
	}

	// --- OIDC Handler ---
	var oidcHandler *auth.OIDCHandler
	if a.authConfig != nil {
		handler, err := auth.NewOIDCHandler(*a.authConfig, sessionManager, identityStore)
		if err != nil {
			return err
		}
		oidcHandler = handler
	}

	// --- Dependencies ---
	a.dependencies = Dependencies{
		DB:            a.db,
		Pool:          a.db.Pool(),
		Session:       sessionManager,
		Auth:          oidcHandler,
		IdentityStore: identityStore,
		OpenAPI:       a.openAPIConfig,
		Logger:        a.logger,
		Clock:         a.clock,
	}

	// --- Middleware ---
	if !a.disableDefaultMiddleware {
		a.router.Use(chiMiddleware.RealIP)
		a.router.Use(chiMiddleware.Logger)
		a.router.Use(chiMiddleware.Recoverer)
	}

	if len(a.corsOrigins) > 0 {
		a.router.Use(cors.Handler(cors.Options{
			AllowedOrigins:   a.corsOrigins,
			AllowedMethods:   []string{"GET", "POST", "PUT", "PATCH", "DELETE", "OPTIONS"},
			AllowedHeaders:   []string{"Accept", "Authorization", "Content-Type", "X-CSRF-Token", "X-Request-ID"},
			ExposedHeaders:   []string{"Link", "X-Request-ID"},
			AllowCredentials: true,
			MaxAge:           300,
		}))
	}

	if a.otelConfig != nil && a.otelConfig.TracingEnabled() {
		a.router.Use(observability.OTELTracingMiddleware(a.otelConfig.ServiceName))
	}
	if a.otelConfig != nil && a.otelConfig.MetricsEnabled() {
		a.router.Use(observability.OTELMetricsMiddleware(a.otelConfig.ServiceName))
	}

	if sessionManager != nil {
		a.router.Use(sessionManager.LoadAndSave)
		sessionKey := auth.DefaultSessionKey
		if a.authConfig != nil && a.authConfig.SessionKey != "" {
			sessionKey = a.authConfig.SessionKey
		}
		a.router.Use(observability.CorrelationAndAudit(sessionManager, sessionKey))
	}

	for _, mw := range a.middleware {
		a.router.Use(mw)
	}

	// --- Auth routes ---
	if oidcHandler != nil {
		a.router.Route("/auth", func(r chi.Router) {
			if a.loginHandler != nil {
				r.Get("/login", a.loginHandler)
			} else {
				r.Get("/login", defaultLoginHandler)
			}
			oidcHandler.RegisterRoutes(r)
		})
	}

	// --- Modules ---
	for _, module := range a.modules {
		if err := module.Register(a.router, a.dependencies); err != nil {
			return err
		}
	}

	return http.ListenAndServe(fmt.Sprintf(":%s", a.port), a.router)
}

// Close performs graceful shutdown of the application's resources.
func (a *Application) Close() error {
	var errs []error
	if err := a.db.Close(); err != nil {
		errs = append(errs, err)
	}
	if a.otelShutdown != nil {
		timeout := 5 * time.Second
		if a.otelConfig != nil {
			timeout = a.otelConfig.WithDefaults().ShutdownTimeout
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

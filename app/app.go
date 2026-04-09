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

type Clock interface {
	Now() time.Time
}

type realClock struct{}

func (realClock) Now() time.Time {
	return time.Now()
}

type Module interface {
	Register(r chi.Router, deps Dependencies) error
}

type ModuleFunc func(r chi.Router, deps Dependencies) error

func (f ModuleFunc) Register(r chi.Router, deps Dependencies) error {
	return f(r, deps)
}

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

type Application struct {
	cfg          Config
	router       *chi.Mux
	db           *db.Manager
	dependencies Dependencies
	modules      []Module
	otelShutdown func(context.Context) error
}

func New(cfg Config, modules ...Module) (*Application, error) {
	withDefaults := cfg.withDefaults()
	if err := withDefaults.validate(); err != nil {
		return nil, err
	}

	database := db.NewManager(withDefaults.Database)
	app := &Application{
		cfg:     withDefaults,
		router:  chi.NewRouter(),
		db:      database,
		modules: modules,
	}
	return app, nil
}

func (a *Application) Router() chi.Router {
	return a.router
}

func (a *Application) Dependencies() Dependencies {
	return a.dependencies
}

func (a *Application) Run(ctx context.Context) error {
	if a.cfg.OTEL.Enabled {
		shutdown, err := observability.SetupOTEL(ctx, a.cfg.OTEL)
		if err != nil {
			return fmt.Errorf("setting up OTEL: %w", err)
		}
		a.otelShutdown = shutdown
		if a.cfg.OTEL.TracingEnabled() {
			a.db.EnableOTELTracing()
		}
	}

	if err := a.db.Connect(ctx); err != nil {
		return err
	}

	if a.cfg.Migrations.EnableCore {
		a.db.AddMigrationSource(db.MigrationSource{FS: migrations.Core(), Dir: "."})
	}
	if a.cfg.Migrations.EnableAuth {
		a.db.AddMigrationSource(db.MigrationSource{FS: migrations.Auth(), Dir: "."})
	}
	for _, source := range a.cfg.Migrations.Sources {
		a.db.AddMigrationSource(source)
	}
	if err := a.db.RunMigrations(ctx); err != nil {
		return err
	}

	sessionManager := session.NewPostgres(a.cfg.Session, a.db.Pool(), "sessions")
	session.RegisterGobTypes(auth.SessionClaims{})

	identityStore := a.cfg.Auth.IdentityStore
	if a.cfg.Auth.OIDC.Enabled && identityStore == nil {
		var err error
		identityStore, err = auth.NewPostgresIdentityStore(a.db.Pool(), auth.PostgresStoreConfig{})
		if err != nil {
			return fmt.Errorf("failed to create identity store: %w", err)
		}
	}

	var oidcHandler *auth.OIDCHandler
	if a.cfg.Auth.OIDC.Enabled {
		handler, err := auth.NewOIDCHandler(a.cfg.Auth.OIDC, sessionManager, identityStore)
		if err != nil {
			return err
		}
		oidcHandler = handler
	}

	a.dependencies = Dependencies{
		DB:            a.db,
		Pool:          a.db.Pool(),
		Session:       sessionManager,
		Auth:          oidcHandler,
		IdentityStore: identityStore,
		OpenAPI:       a.cfg.OpenAPI,
		Logger:        slog.Default(),
		Clock:         realClock{},
	}

	a.router.Use(chiMiddleware.RealIP)
	a.router.Use(chiMiddleware.Logger)
	a.router.Use(chiMiddleware.Recoverer)

	if a.cfg.CORS.Enabled() {
		corsCfg := a.cfg.CORS
		a.router.Use(cors.Handler(cors.Options{
			AllowedOrigins:   corsCfg.AllowedOrigins,
			AllowedMethods:   corsCfg.AllowedMethods,
			AllowedHeaders:   corsCfg.AllowedHeaders,
			ExposedHeaders:   corsCfg.ExposedHeaders,
			AllowCredentials: *corsCfg.AllowCredentials,
			MaxAge:           corsCfg.MaxAge,
		}))
	}

	if a.cfg.OTEL.TracingEnabled() {
		a.router.Use(observability.OTELTracingMiddleware(a.cfg.OTEL.ServiceName))
	}
	if a.cfg.OTEL.MetricsEnabled() {
		a.router.Use(observability.OTELMetricsMiddleware(a.cfg.OTEL.ServiceName))
	}

	a.router.Use(sessionManager.LoadAndSave)
	a.router.Use(observability.CorrelationAndAudit(sessionManager, a.cfg.Auth.OIDC.SessionKey))

	if oidcHandler != nil {
		a.router.Route("/auth", func(r chi.Router) {
			if a.cfg.Auth.LoginHandler != nil {
				r.Get("/login", a.cfg.Auth.LoginHandler)
			} else {
				r.Get("/login", defaultLoginHandler)
			}
			oidcHandler.RegisterRoutes(r)
		})
	}

	for _, module := range a.modules {
		if err := module.Register(a.router, a.dependencies); err != nil {
			return err
		}
	}

	return http.ListenAndServe(fmt.Sprintf(":%s", a.cfg.Server.Port), a.router)
}

func (a *Application) Close() error {
	var errs []error
	if err := a.db.Close(); err != nil {
		errs = append(errs, err)
	}
	if a.otelShutdown != nil {
		timeout := a.cfg.OTEL.WithDefaults().ShutdownTimeout
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

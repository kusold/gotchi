package runtime

import (
	"context"
	"fmt"
	"net/http"

	"github.com/go-chi/chi/v5"

	"github.com/kusold/gotchi/auth"
	"github.com/kusold/gotchi/db"
	"github.com/kusold/gotchi/migrations"
	"github.com/kusold/gotchi/openapi"
	"github.com/kusold/gotchi/session"
)

type Config struct {
	DatabaseURL           string
	DatabaseEnableTracing bool
	Session               session.Config
	Auth                  auth.Config
	OpenAPI               openapi.Config
}

type Dependencies struct {
	DB      *db.Manager
	Session *session.Manager
	OIDC    *auth.OIDCHandler
	OpenAPI openapi.Config
}

type SetupRoutesFunc func(r chi.Router, deps Dependencies) error

type Options struct {
	Config Config
	Port   string

	MigrationSources             []db.MigrationSource
	EnableCoreBaselineMigrations bool
	EnableAuthBaselineMigrations bool

	AuthHooks   auth.Hooks
	SetupRoutes SetupRoutesFunc
}

type Server struct {
	port   string
	router *chi.Mux

	cfg   Config
	db    *db.Manager
	deps  Dependencies
	setup SetupRoutesFunc

	migrationSources             []db.MigrationSource
	enableCoreBaselineMigrations bool
	enableAuthBaselineMigrations bool
	authHooks                    auth.Hooks
}

func NewServer(opts Options) (*Server, error) {
	if opts.SetupRoutes == nil {
		return nil, fmt.Errorf("setup routes function is required")
	}
	if opts.Config.DatabaseURL == "" {
		return nil, fmt.Errorf("database URL is required")
	}

	database := db.NewManager(db.Config{
		DatabaseURL:   opts.Config.DatabaseURL,
		EnableTracing: opts.Config.DatabaseEnableTracing,
	})

	return &Server{
		port:                         opts.Port,
		router:                       chi.NewRouter(),
		cfg:                          opts.Config,
		db:                           database,
		setup:                        opts.SetupRoutes,
		migrationSources:             opts.MigrationSources,
		enableCoreBaselineMigrations: opts.EnableCoreBaselineMigrations,
		enableAuthBaselineMigrations: opts.EnableAuthBaselineMigrations,
		authHooks:                    opts.AuthHooks,
	}, nil
}

func (s *Server) Router() chi.Router {
	return s.router
}

func (s *Server) Start(ctx context.Context) error {
	if err := s.db.Connect(ctx); err != nil {
		return err
	}

	if s.enableCoreBaselineMigrations {
		s.db.AddMigrationSource(db.MigrationSource{FS: migrations.Core(), Dir: "."})
	}
	if s.enableAuthBaselineMigrations {
		s.db.AddMigrationSource(db.MigrationSource{FS: migrations.Auth(), Dir: "."})
	}
	for _, source := range s.migrationSources {
		s.db.AddMigrationSource(source)
	}
	if err := s.db.RunMigrations(ctx); err != nil {
		return err
	}

	sessionManager := session.NewPostgres(s.cfg.Session, s.db.Pool(), "sessions")
	session.RegisterGobTypes(auth.SessionClaims{})

	var oidcHandler *auth.OIDCHandler
	if s.cfg.Auth.Enabled {
		if s.authHooks == nil {
			return fmt.Errorf("auth hooks are required when auth is enabled")
		}
		handler, err := auth.NewOIDCHandler(s.cfg.Auth, sessionManager, s.authHooks)
		if err != nil {
			return err
		}
		oidcHandler = handler
	}

	s.deps = Dependencies{
		DB:      s.db,
		Session: sessionManager,
		OIDC:    oidcHandler,
		OpenAPI: s.cfg.OpenAPI,
	}

	if err := s.setup(s.router, s.deps); err != nil {
		return err
	}

	return http.ListenAndServe(fmt.Sprintf(":%s", s.port), s.router)
}

func (s *Server) Close() {
	s.db.Close()
}

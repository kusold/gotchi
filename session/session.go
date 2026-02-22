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

const DefaultSessionKey = "session"

type Config struct {
	ExpiryInterval time.Duration
	Lifetime       time.Duration
	CookieName     string
	CookieSecure   bool
	CookieSameSite http.SameSite
}

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

type Manager struct {
	sessionManager *scs.SessionManager
}

func RegisterGobTypes(values ...any) {
	for _, value := range values {
		gob.Register(value)
	}
}

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

func NewMemory(cfg Config) *Manager {
	c := cfg.withDefaults()
	store := memstore.NewWithCleanupInterval(c.ExpiryInterval)
	return New(c, store)
}

func NewPostgres(cfg Config, pool *pgxpool.Pool, tableName string) *Manager {
	c := cfg.withDefaults()
	storeCfg := pgxstore.Config{CleanUpInterval: c.ExpiryInterval}
	if tableName != "" {
		storeCfg.TableName = tableName
	}
	store := pgxstore.NewWithConfig(pool, storeCfg)
	return New(c, store)
}

func (m *Manager) LoadAndSave(next http.Handler) http.Handler {
	return m.sessionManager.LoadAndSave(next)
}

func (m *Manager) Load(ctx context.Context, token string) (context.Context, error) {
	return m.sessionManager.Load(ctx, token)
}

func (m *Manager) Get(ctx context.Context, key string) any {
	return m.sessionManager.Get(ctx, key)
}

func (m *Manager) GetString(ctx context.Context, key string) string {
	return m.sessionManager.GetString(ctx, key)
}

func (m *Manager) Put(ctx context.Context, key string, value any) {
	m.sessionManager.Put(ctx, key, value)
}

func (m *Manager) PutHTTP(r *http.Request, key string, value any) {
	m.sessionManager.Put(r.Context(), key, value)
}

func (m *Manager) Destroy(ctx context.Context) error {
	return m.sessionManager.Destroy(ctx)
}

func (m *Manager) Inner() *scs.SessionManager {
	return m.sessionManager
}

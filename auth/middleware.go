package auth

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"

	"github.com/google/uuid"

	"github.com/kusold/gotchi/session"
	"github.com/kusold/gotchi/tenantctx"
)

type Mode string

const (
	ModeAPI Mode = "api"
	ModeUI  Mode = "ui"
)

type MiddlewareConfig struct {
	Mode                    Mode
	SessionKey              string
	LoginPath               string
	TenantPickerPath        string
	AllowPathsWithoutTenant []string
	LegacyTenantContextKey  any
	LegacyClaimsContextKey  any
}

func (c MiddlewareConfig) withDefaults() MiddlewareConfig {
	cfg := c
	if cfg.SessionKey == "" {
		cfg.SessionKey = DefaultSessionKey
	}
	if cfg.LoginPath == "" {
		cfg.LoginPath = DefaultLoginPath
	}
	if cfg.TenantPickerPath == "" {
		cfg.TenantPickerPath = DefaultTenantPickerPath
	}
	if cfg.Mode == "" {
		cfg.Mode = ModeAPI
	}
	if len(cfg.AllowPathsWithoutTenant) == 0 {
		cfg.AllowPathsWithoutTenant = []string{DefaultTenantPickerPath, "/auth/tenant/select"}
	}
	return cfg
}

func RequireAuthenticated(sessionManager *session.Manager, cfg MiddlewareConfig) func(http.Handler) http.Handler {
	conf := cfg.withDefaults()

	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			claims, ok := sessionManager.Get(r.Context(), conf.SessionKey).(SessionClaims)
			if !ok || !claims.Authenticated {
				handleUnauthenticated(w, r, conf)
				return
			}

			if claims.ActiveTenantID == nil && !isTenantOptionalPath(r.URL.Path, conf.AllowPathsWithoutTenant) {
				handleTenantRequired(w, r, conf)
				return
			}

			ctx := WithSessionClaims(r.Context(), claims)
			if tenantID, ok := activeTenantFromClaims(claims); ok {
				ctx = tenantctx.WithTenantID(ctx, tenantID)
				if conf.LegacyTenantContextKey != nil {
					ctx = context.WithValue(ctx, conf.LegacyTenantContextKey, tenantID.String())
				}
			}
			if conf.LegacyClaimsContextKey != nil {
				ctx = context.WithValue(ctx, conf.LegacyClaimsContextKey, claims)
			}

			next.ServeHTTP(w, r.WithContext(ctx))
		})
	}
}

func handleUnauthenticated(w http.ResponseWriter, r *http.Request, cfg MiddlewareConfig) {
	if cfg.Mode == ModeUI {
		http.Redirect(w, r, cfg.LoginPath, http.StatusSeeOther)
		return
	}
	http.Error(w, "unauthorized", http.StatusUnauthorized)
}

func handleTenantRequired(w http.ResponseWriter, r *http.Request, cfg MiddlewareConfig) {
	if cfg.Mode == ModeUI {
		http.Redirect(w, r, cfg.TenantPickerPath, http.StatusSeeOther)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusConflict)
	json.NewEncoder(w).Encode(map[string]any{
		"error":                     "tenant_selection_required",
		"tenant_selection_required": true,
	})
}

func isTenantOptionalPath(path string, allowPaths []string) bool {
	for _, allowPath := range allowPaths {
		if allowPath == "" {
			continue
		}
		if strings.HasSuffix(allowPath, "*") {
			prefix := strings.TrimSuffix(allowPath, "*")
			if strings.HasPrefix(path, prefix) {
				return true
			}
			continue
		}
		if path == allowPath {
			return true
		}
	}
	return false
}

func ActiveTenantID(ctx context.Context) (uuid.UUID, bool) {
	return tenantctx.TenantID(ctx)
}

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

// Mode determines how the authentication middleware handles unauthenticated
// users and tenant selection requirements.
type Mode string

const (
	// ModeAPI returns HTTP 401/409 JSON responses for unauthenticated or
	// tenant-less requests.
	ModeAPI Mode = "api"
	// ModeUI redirects to login or tenant picker pages for unauthenticated
	// or tenant-less requests.
	ModeUI Mode = "ui"
)

// MiddlewareConfig controls the behavior of [RequireAuthenticated] middleware.
type MiddlewareConfig struct {
	// Mode determines how unauthenticated/tenant-less requests are handled.
	// Defaults to ModeAPI.
	Mode Mode
	// SessionKey is the session key where auth claims are stored.
	// Defaults to [DefaultSessionKey].
	SessionKey string
	// LoginPath is the URL to redirect to when Mode is ModeUI.
	// Defaults to [DefaultLoginPath].
	LoginPath string
	// TenantPickerPath is the URL to redirect to when a tenant selection
	// is required and Mode is ModeUI. Defaults to [DefaultTenantPickerPath].
	TenantPickerPath string
	// AllowPathsWithoutTenant is a list of URL paths that are accessible
	// without an active tenant. Supports "*" suffix for prefix matching
	// (e.g. "/auth/*"). Defaults to the tenant picker and select paths.
	AllowPathsWithoutTenant []string
	// LegacyTenantContextKey, when non-nil, causes the tenant ID to be set
	// in context under this key for backward compatibility.
	LegacyTenantContextKey any
	// LegacyClaimsContextKey, when non-nil, causes the SessionClaims to be
	// set in context under this key for backward compatibility.
	LegacyClaimsContextKey any
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

// RequireAuthenticated returns HTTP middleware that enforces authentication.
// It checks for valid [SessionClaims] in the session and, if a tenant is
// required, ensures one is selected. On success it sets the claims and tenant
// ID in the request context via [WithSessionClaims] and [tenantctx.WithTenantID].
//
// In ModeAPI (default), unauthenticated requests receive 401 and tenant-less
// requests receive 409 with a JSON body. In ModeUI, they are redirected to
// the login page or tenant picker respectively.
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

// ActiveTenantID extracts the active tenant ID from the context. This is a
// convenience wrapper around [tenantctx.TenantID].
func ActiveTenantID(ctx context.Context) (uuid.UUID, bool) {
	return tenantctx.TenantID(ctx)
}

package auth

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func assertDefaultConfig(t *testing.T, cfg Config) {
	t.Helper()
	assert.Equal(t, DefaultLoginPath, cfg.LoginPath, "LoginPath should default to DefaultLoginPath")
	assert.Equal(t, DefaultPostLoginRedirect, cfg.PostLoginRedirect, "PostLoginRedirect should default to DefaultPostLoginRedirect")
	assert.Equal(t, DefaultTenantPickerPath, cfg.TenantPickerPath, "TenantPickerPath should default to DefaultTenantPickerPath")
	assert.Equal(t, DefaultSessionKey, cfg.SessionKey, "SessionKey should default to DefaultSessionKey")
	assert.Equal(t, DefaultAuthorizePath, cfg.AuthorizePath, "AuthorizePath should default to DefaultAuthorizePath")
	assert.Equal(t, DefaultCallbackPath, cfg.CallbackPath, "CallbackPath should default to DefaultCallbackPath")
	assert.Equal(t, DefaultTenantsPath, cfg.TenantsPath, "TenantsPath should default to DefaultTenantsPath")
	assert.Equal(t, DefaultTenantSelectPath, cfg.TenantSelectPath, "TenantSelectPath should default to DefaultTenantSelectPath")
	assert.Equal(t, DefaultLogoutPath, cfg.LogoutPath, "LogoutPath should default to DefaultLogoutPath")
	assert.Equal(t, DefaultPostLogoutRedirect, cfg.PostLogoutRedirect, "PostLogoutRedirect should default to DefaultPostLogoutRedirect")
	assert.Equal(t, DefaultStateCookieName, cfg.StateCookieName, "StateCookieName should default to DefaultStateCookieName")
	assert.NotNil(t, cfg.CookieSecure, "CookieSecure should not be nil")
	assert.True(t, *cfg.CookieSecure, "CookieSecure should default to true")
}

func TestConfig_WithDefaults(t *testing.T) {
	cfg := Config{}.withDefaults()
	assertDefaultConfig(t, cfg)
}

func TestConfig_WithDefaults_PublicMethod(t *testing.T) {
	cfg := Config{}.WithDefaults()
	assertDefaultConfig(t, cfg)
}

func TestConfig_WithDefaults_PreservesProvidedValues(t *testing.T) {
	customSecure := false
	cfg := Config{
		Enabled:           true,
		IssuerURL:         "https://custom.example.com",
		ClientID:          "custom-client-id",
		ClientSecret:      "custom-secret",
		RedirectURL:       "https://custom.example.com/callback",
		LoginPath:         "/custom/login",
		PostLoginRedirect: "/custom/dashboard",
		TenantPickerPath:  "/custom/tenants",
		SessionKey:        "custom-session",
		AuthorizePath:     "/custom/authorize",
		CallbackPath:      "/custom/callback",
		TenantsPath:       "/custom/tenants/list",
		TenantSelectPath:  "/custom/tenant/select",
		LogoutPath:        "/custom/logout",
		PostLogoutRedirect: "/custom/goodbye",
		StateCookieName:   "custom_state",
		CookieSecure:      &customSecure,
	}.withDefaults()

	assert.Equal(t, true, cfg.Enabled)
	assert.Equal(t, "https://custom.example.com", cfg.IssuerURL)
	assert.Equal(t, "custom-client-id", cfg.ClientID)
	assert.Equal(t, "custom-secret", cfg.ClientSecret)
	assert.Equal(t, "https://custom.example.com/callback", cfg.RedirectURL)
	assert.Equal(t, "/custom/login", cfg.LoginPath)
	assert.Equal(t, "/custom/dashboard", cfg.PostLoginRedirect)
	assert.Equal(t, "/custom/tenants", cfg.TenantPickerPath)
	assert.Equal(t, "custom-session", cfg.SessionKey)
	assert.Equal(t, "/custom/authorize", cfg.AuthorizePath)
	assert.Equal(t, "/custom/callback", cfg.CallbackPath)
	assert.Equal(t, "/custom/tenants/list", cfg.TenantsPath)
	assert.Equal(t, "/custom/tenant/select", cfg.TenantSelectPath)
	assert.Equal(t, "/custom/logout", cfg.LogoutPath)
	assert.Equal(t, "/custom/goodbye", cfg.PostLogoutRedirect)
	assert.Equal(t, "custom_state", cfg.StateCookieName)
	assert.NotNil(t, cfg.CookieSecure)
	assert.False(t, *cfg.CookieSecure)
}

func TestConfig_WithDefaults_PartialValues(t *testing.T) {
	cfg := Config{
		LoginPath:     "/custom-login",
		SessionKey:    "",
		AuthorizePath: "/custom-auth",
		CookieSecure:  nil,
	}.withDefaults()

	assert.Equal(t, "/custom-login", cfg.LoginPath, "non-empty LoginPath should be preserved")
	assert.Equal(t, DefaultSessionKey, cfg.SessionKey, "empty SessionKey should be defaulted")
	assert.Equal(t, "/custom-auth", cfg.AuthorizePath, "non-empty AuthorizePath should be preserved")
	assert.Equal(t, DefaultPostLoginRedirect, cfg.PostLoginRedirect, "empty PostLoginRedirect should be defaulted")
	assert.NotNil(t, cfg.CookieSecure)
	assert.True(t, *cfg.CookieSecure, "nil CookieSecure should default to true")
}

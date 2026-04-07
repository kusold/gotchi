package auth

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestConfig_WithDefaults(t *testing.T) {
	cfg := Config{}.withDefaults()

	assert.Equal(t, DefaultLoginPath, cfg.LoginPath, "LoginPath should default to DefaultLoginPath")
	assert.Equal(t, DefaultPostLoginRedirect, cfg.PostLoginRedirect, "PostLoginRedirect should default to DefaultPostLoginRedirect")
	assert.Equal(t, DefaultTenantPickerPath, cfg.TenantPickerPath, "TenantPickerPath should default to DefaultTenantPickerPath")
	assert.Equal(t, DefaultSessionKey, cfg.SessionKey, "SessionKey should default to DefaultSessionKey")
	assert.Equal(t, DefaultAuthorizePath, cfg.AuthorizePath, "AuthorizePath should default to DefaultAuthorizePath")
	assert.Equal(t, DefaultCallbackPath, cfg.CallbackPath, "CallbackPath should default to DefaultCallbackPath")
	assert.Equal(t, DefaultTenantsPath, cfg.TenantsPath, "TenantsPath should default to DefaultTenantsPath")
	assert.Equal(t, DefaultTenantSelectPath, cfg.TenantSelectPath, "TenantSelectPath should default to DefaultTenantSelectPath")
	assert.Equal(t, DefaultStateCookieName, cfg.StateCookieName, "StateCookieName should default to DefaultStateCookieName")
	assert.NotNil(t, cfg.CookieSecure, "CookieSecure should not be nil")
	assert.True(t, *cfg.CookieSecure, "CookieSecure should default to true")
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
		StateCookieName:   "custom_state",
		CookieSecure:      &customSecure,
	}.withDefaults()

	// Verify all custom values are preserved
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
	assert.Equal(t, "custom_state", cfg.StateCookieName)
	assert.NotNil(t, cfg.CookieSecure)
	assert.False(t, *cfg.CookieSecure)
}

func TestConfig_WithDefaults_PublicMethod(t *testing.T) {
	// Test that the public WithDefaults() method calls withDefaults()
	cfg := Config{}.WithDefaults()

	assert.Equal(t, DefaultLoginPath, cfg.LoginPath)
	assert.Equal(t, DefaultPostLoginRedirect, cfg.PostLoginRedirect)
	assert.Equal(t, DefaultTenantPickerPath, cfg.TenantPickerPath)
	assert.Equal(t, DefaultSessionKey, cfg.SessionKey)
	assert.Equal(t, DefaultAuthorizePath, cfg.AuthorizePath)
	assert.Equal(t, DefaultCallbackPath, cfg.CallbackPath)
	assert.Equal(t, DefaultTenantsPath, cfg.TenantsPath)
	assert.Equal(t, DefaultTenantSelectPath, cfg.TenantSelectPath)
	assert.Equal(t, DefaultStateCookieName, cfg.StateCookieName)
	assert.NotNil(t, cfg.CookieSecure)
	assert.True(t, *cfg.CookieSecure)
}

func TestConfig_WithDefaults_PartialValues(t *testing.T) {
	// Test that only empty string values are replaced, non-empty values preserved
	cfg := Config{
		LoginPath:        "/custom-login",
		SessionKey:       "", // Empty, should be defaulted
		AuthorizePath:    "/custom-auth",
		CookieSecure:     nil, // nil, should be defaulted to true
	}.withDefaults()

	assert.Equal(t, "/custom-login", cfg.LoginPath, "non-empty LoginPath should be preserved")
	assert.Equal(t, DefaultSessionKey, cfg.SessionKey, "empty SessionKey should be defaulted")
	assert.Equal(t, "/custom-auth", cfg.AuthorizePath, "non-empty AuthorizePath should be preserved")
	assert.Equal(t, DefaultPostLoginRedirect, cfg.PostLoginRedirect, "empty PostLoginRedirect should be defaulted")
	assert.NotNil(t, cfg.CookieSecure)
	assert.True(t, *cfg.CookieSecure, "nil CookieSecure should default to true")
}

func TestConfig_WithDefaults_AllPathDefaults(t *testing.T) {
	// Verify all the default constants are applied correctly
	tests := []struct {
		name     string
		field    string
		expected string
	}{
		{"LoginPath", DefaultLoginPath, "/auth/login"},
		{"PostLoginRedirect", DefaultPostLoginRedirect, "/ui/profile"},
		{"TenantPickerPath", DefaultTenantPickerPath, "/auth/tenants"},
		{"SessionKey", DefaultSessionKey, "auth"},
		{"AuthorizePath", DefaultAuthorizePath, "/oidc/authorize"},
		{"CallbackPath", DefaultCallbackPath, "/oidc/callback"},
		{"TenantsPath", DefaultTenantsPath, "/tenants"},
		{"TenantSelectPath", DefaultTenantSelectPath, "/tenant/select"},
		{"StateCookieName", DefaultStateCookieName, "oidc_state"},
	}

	cfg := Config{}.withDefaults()

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			switch tt.name {
			case "LoginPath":
				assert.Equal(t, tt.expected, cfg.LoginPath)
			case "PostLoginRedirect":
				assert.Equal(t, tt.expected, cfg.PostLoginRedirect)
			case "TenantPickerPath":
				assert.Equal(t, tt.expected, cfg.TenantPickerPath)
			case "SessionKey":
				assert.Equal(t, tt.expected, cfg.SessionKey)
			case "AuthorizePath":
				assert.Equal(t, tt.expected, cfg.AuthorizePath)
			case "CallbackPath":
				assert.Equal(t, tt.expected, cfg.CallbackPath)
			case "TenantsPath":
				assert.Equal(t, tt.expected, cfg.TenantsPath)
			case "TenantSelectPath":
				assert.Equal(t, tt.expected, cfg.TenantSelectPath)
			case "StateCookieName":
				assert.Equal(t, tt.expected, cfg.StateCookieName)
			}
		})
	}
}

package auth

// Config holds all configuration for OIDC authentication. Path fields default
// to the corresponding Default* constants when empty. Use [Config.WithDefaults]
// to populate defaults explicitly.
type Config struct {
	// Enabled controls whether OIDC authentication is active.
	Enabled bool
	// IssuerURL is the OIDC provider's issuer URL (e.g. "https://accounts.google.com").
	// Required when Enabled is true.
	IssuerURL string
	// ClientID is the OAuth2 client ID registered with the OIDC provider.
	// Required when Enabled is true.
	ClientID string
	// ClientSecret is the OAuth2 client secret registered with the OIDC provider.
	// Required when Enabled is true.
	ClientSecret string
	// RedirectURL is the callback URL that the OIDC provider redirects to after
	// authentication (e.g. "https://myapp.com/oidc/callback"). Required when
	// Enabled is true.
	RedirectURL string

	// LoginPath is the URL path for the login page. Defaults to [DefaultLoginPath].
	LoginPath string
	// PostLoginRedirect is the URL to redirect to after successful login.
	// Defaults to [DefaultPostLoginRedirect].
	PostLoginRedirect string
	// TenantPickerPath is the URL path for the tenant selection UI.
	// Defaults to [DefaultTenantPickerPath].
	TenantPickerPath string
	// SessionKey is the key used to store claims in the session.
	// Defaults to [DefaultSessionKey].
	SessionKey string

	// AuthorizePath is the URL path that initiates the OIDC authorize redirect.
	// Defaults to [DefaultAuthorizePath].
	AuthorizePath string
	// CallbackPath is the URL path that handles the OIDC callback.
	// Defaults to [DefaultCallbackPath].
	CallbackPath string
	// TenantsPath is the URL path for the API endpoint listing a user's tenants.
	// Defaults to [DefaultTenantsPath].
	TenantsPath string
	// TenantSelectPath is the URL path for the API endpoint to select a tenant.
	// Defaults to [DefaultTenantSelectPath].
	TenantSelectPath string
	// StateCookieName is the name of the cookie holding the OIDC state parameter.
	// Defaults to [DefaultStateCookieName].
	StateCookieName string
	// CookieSecure controls the Secure flag on the state cookie.
	// Defaults to true for security. Set to false only for local development.
	CookieSecure *bool
}

func (c Config) withDefaults() Config {
	cfg := c
	if cfg.LoginPath == "" {
		cfg.LoginPath = DefaultLoginPath
	}
	if cfg.PostLoginRedirect == "" {
		cfg.PostLoginRedirect = DefaultPostLoginRedirect
	}
	if cfg.TenantPickerPath == "" {
		cfg.TenantPickerPath = DefaultTenantPickerPath
	}
	if cfg.SessionKey == "" {
		cfg.SessionKey = DefaultSessionKey
	}
	if cfg.AuthorizePath == "" {
		cfg.AuthorizePath = DefaultAuthorizePath
	}
	if cfg.CallbackPath == "" {
		cfg.CallbackPath = DefaultCallbackPath
	}
	if cfg.TenantsPath == "" {
		cfg.TenantsPath = DefaultTenantsPath
	}
	if cfg.TenantSelectPath == "" {
		cfg.TenantSelectPath = DefaultTenantSelectPath
	}
	if cfg.StateCookieName == "" {
		cfg.StateCookieName = DefaultStateCookieName
	}
	if cfg.CookieSecure == nil {
		secure := true
		cfg.CookieSecure = &secure
	}
	return cfg
}

// WithDefaults returns a copy of Config with empty fields populated by their
// default values.
func (c Config) WithDefaults() Config {
	return c.withDefaults()
}

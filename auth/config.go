package auth

type Config struct {
	Enabled      bool
	IssuerURL    string
	ClientID     string
	ClientSecret string
	RedirectURL  string

	LoginPath         string
	PostLoginRedirect string
	TenantPickerPath  string
	SessionKey        string

	AuthorizePath    string
	CallbackPath     string
	TenantsPath      string
	TenantSelectPath string
	StateCookieName  string
	// CookieSecure controls the Secure flag on the state cookie.
	// Defaults to true for security. Set to false only for development
	// or when behind a TLS-terminating proxy that handles HTTPS.
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

func (c Config) WithDefaults() Config {
	return c.withDefaults()
}

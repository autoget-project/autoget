package auth

import (
	"fmt"
	"net/url"
)

// Config is the optional auth section of the app config.
type Config struct {
	Issuer       string `yaml:"issuer"`
	ClientID     string `yaml:"client_id"`
	ClientSecret string `yaml:"client_secret"`
	// RedirectBaseURL is the externally reachable base URL of this app,
	// e.g. https://autoget.example.com. The OAuth callback is
	// {RedirectBaseURL}/auth/callback.
	RedirectBaseURL string `yaml:"redirect_base_url"`
	// RequiredRole, when non-empty, requires the JWT "roles" claim (a space-separated
	// list of roles) to contain this role. Surrounding whitespace is ignored. Omit or
	// leave empty to skip role validation.
	RequiredRole string `yaml:"required_role"`
}

// Validate checks the auth config fields.
func (c *Config) Validate() error {
	if err := requireAbsoluteURL("auth issuer", c.Issuer); err != nil {
		return err
	}
	if c.ClientID == "" {
		return fmt.Errorf("auth client_id is required")
	}
	if c.ClientSecret == "" {
		return fmt.Errorf("auth client_secret is required")
	}
	if err := requireAbsoluteURL("auth redirect_base_url", c.RedirectBaseURL); err != nil {
		return err
	}
	return nil
}

// requireAbsoluteURL rejects values that would silently produce a broken
// endpoint, such as a bare host name or a relative path.
func requireAbsoluteURL(name, value string) error {
	if value == "" {
		return fmt.Errorf("%s is required", name)
	}
	u, err := url.Parse(value)
	if err != nil || u.Scheme == "" || u.Host == "" {
		return fmt.Errorf("%s must be an absolute URL like https://host, got %q", name, value)
	}
	return nil
}

// tokenResponse is what the frontend receives after login and refresh.
type tokenResponse struct {
	AccessToken  string `json:"access_token"`
	RefreshToken string `json:"refresh_token"`
	IDToken      string `json:"id_token"`
	// ExpiresIn is the access token lifetime in seconds (RFC 6749 §5.1).
	ExpiresIn int64  `json:"expires_in"`
	TokenType string `json:"token_type"`
}

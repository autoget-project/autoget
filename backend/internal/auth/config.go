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
}

// Validate checks the auth config fields.
func (c *Config) Validate() error {
	if c.Issuer == "" {
		return fmt.Errorf("auth issuer is required")
	}
	if c.ClientID == "" {
		return fmt.Errorf("auth client_id is required")
	}
	if c.ClientSecret == "" {
		return fmt.Errorf("auth client_secret is required")
	}
	if c.RedirectBaseURL == "" {
		return fmt.Errorf("auth redirect_base_url is required")
	}
	if _, err := url.Parse(c.RedirectBaseURL); err != nil {
		return fmt.Errorf("invalid auth redirect_base_url: %w", err)
	}
	return nil
}

// tokenResponse is what the frontend receives after login and refresh.
type tokenResponse struct {
	AccessToken  string `json:"access_token"`
	RefreshToken string `json:"refresh_token"`
	IDToken      string `json:"id_token"`
	ExpiresIn    int64  `json:"expires_in"`
	TokenType    string `json:"token_type"`
}

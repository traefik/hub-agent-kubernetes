/*
Copyright (C) 2022 Traefik Labs

This program is free software: you can redistribute it and/or modify
it under the terms of the GNU Affero General Public License as published
by the Free Software Foundation, either version 3 of the License, or
(at your option) any later version.

This program is distributed in the hope that it will be useful,
but WITHOUT ANY WARRANTY; without even the implied warranty of
MERCHANTABILITY or FITNESS FOR A PARTICULAR PURPOSE.  See the
GNU Affero General Public License for more details.

You should have received a copy of the GNU Affero General Public License
along with this program. If not, see <https://www.gnu.org/licenses/>.
*/

package oidc

import (
	"context"
	"errors"
	"fmt"

	"github.com/coreos/go-oidc/v3/oidc"
)

// Config holds the configuration for the OIDC middleware.
type Config struct {
	Issuer       string `json:"issuer,omitempty"  toml:"issuer,omitempty" yaml:"issuer,omitempty"`
	ClientID     string `json:"clientId,omitempty"  toml:"clientId,omitempty" yaml:"clientId,omitempty"`
	ClientSecret string `json:"clientSecret,omitempty"  toml:"clientSecret,omitempty" yaml:"clientSecret,omitempty"`
	TLS          *TLS   `json:"tls,omitempty" toml:"tls,omitempty" yaml:"tls,omitempty"`

	RedirectURL string            `json:"redirectUrl,omitempty"  toml:"redirectUrl,omitempty" yaml:"redirectUrl,omitempty"`
	LogoutURL   string            `json:"logoutUrl,omitempty"  toml:"logoutUrl,omitempty" yaml:"logoutUrl,omitempty"`
	Scopes      []string          `json:"scopes,omitempty" toml:"scopes,omitempty" yaml:"scopes,omitempty"`
	AuthParams  map[string]string `json:"authParams,omitempty" toml:"authParams,omitempty" yaml:"authParams,omitempty"`
	StateCookie *AuthStateCookie  `json:"stateCookie,omitempty" toml:"stateCookie,omitempty" yaml:"stateCookie,omitempty"`
	Session     *AuthSession      `json:"session,omitempty" toml:"session,omitempty" yaml:"session,omitempty"`

	// ForwardHeaders defines headers that should be added to the request and populated with values extracted from the ID token.
	ForwardHeaders map[string]string `json:"forwardHeaders,omitempty" toml:"forwardHeaders,omitempty" yaml:"forwardHeaders,omitempty"`
	// Claims defines an expression to perform validation on the ID token. For example:
	//     Equals(`grp`, `admin`) && Equals(`scope`, `deploy`)
	Claims string `json:"claims,omitempty" toml:"claims,omitempty" yaml:"claims,omitempty"`
}

// AuthStateCookie carries the state cookie configuration.
type AuthStateCookie struct {
	Secret   string `json:"secret,omitempty" toml:"secret,omitempty" yaml:"secret,omitempty"`
	Path     string `json:"path,omitempty" toml:"path,omitempty" yaml:"path,omitempty"`
	Domain   string `json:"domain,omitempty" toml:"domain,omitempty" yaml:"domain,omitempty"`
	SameSite string `json:"sameSite,omitempty" toml:"sameSite,omitempty" yaml:"sameSite,omitempty"`
	Secure   bool   `json:"secure,omitempty" toml:"secure,omitempty" yaml:"secure,omitempty"`
}

// AuthSession carries session and session cookie configuration.
type AuthSession struct {
	Secret   string `json:"secret,omitempty" toml:"secret,omitempty" yaml:"secret,omitempty"`
	Path     string `json:"path,omitempty" toml:"path,omitempty" yaml:"path,omitempty"`
	Domain   string `json:"domain,omitempty" toml:"domain,omitempty" yaml:"domain,omitempty"`
	SameSite string `json:"sameSite,omitempty" toml:"sameSite,omitempty" yaml:"sameSite,omitempty"`
	Secure   bool   `json:"secure,omitempty" toml:"secure,omitempty" yaml:"secure,omitempty"`
	Refresh  *bool  `json:"refresh,omitempty" toml:"refresh,omitempty" yaml:"refresh,omitempty"`
}

// ApplyDefaultValues applies default values on the given dynamic configuration.
func ApplyDefaultValues(cfg *Config) {
	if len(cfg.Scopes) == 0 {
		cfg.Scopes = []string{"openid"}
	}

	if cfg.StateCookie == nil {
		cfg.StateCookie = &AuthStateCookie{}
	}

	if cfg.StateCookie.Path == "" {
		cfg.StateCookie.Path = "/"
	}

	if cfg.StateCookie.SameSite == "" {
		cfg.StateCookie.SameSite = "lax"
	}

	if cfg.Session == nil {
		cfg.Session = &AuthSession{}
	}

	if cfg.Session.Path == "" {
		cfg.Session.Path = "/"
	}

	if cfg.Session.SameSite == "" {
		cfg.Session.SameSite = "lax"
	}

	if cfg.Session.Refresh == nil {
		cfg.Session.Refresh = ptrBool(true)
	}
}

// Validate validates configuration.
func (cfg *Config) Validate() error {
	ApplyDefaultValues(cfg)

	if cfg.Issuer == "" {
		return errors.New("missing issuer")
	}

	if cfg.ClientID == "" {
		return errors.New("missing client ID")
	}

	if cfg.ClientSecret == "" {
		return errors.New("missing client secret")
	}

	if cfg.Session.Secret == "" {
		return errors.New("missing session secret")
	}

	switch len(cfg.Session.Secret) {
	case 16, 24, 32:
		break
	default:
		return errors.New("session secret must be 16, 24 or 32 characters long")
	}

	if cfg.StateCookie.Secret == "" {
		return errors.New("missing state secret")
	}

	switch len(cfg.StateCookie.Secret) {
	case 16, 24, 32:
		break
	default:
		return errors.New("state secret must be 16, 24 or 32 characters long")
	}

	if cfg.RedirectURL == "" {
		return errors.New("missing redirect URL")
	}

	return nil
}

// ptrBool returns a pointer to boolean.
func ptrBool(v bool) *bool {
	return &v
}

// BuildProvider returns a provider instance from given auth source.
func BuildProvider(ctx context.Context, cfg *Config) (*oidc.Provider, error) {
	provider, err := oidc.NewProvider(ctx, cfg.Issuer)
	if err != nil {
		return nil, fmt.Errorf("unable to create provider: %w", err)
	}

	return provider, nil
}

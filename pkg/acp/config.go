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

package acp

import (
	"github.com/traefik/hub-agent-kubernetes/pkg/acp/basicauth"
	"github.com/traefik/hub-agent-kubernetes/pkg/acp/jwt"
	"github.com/traefik/hub-agent-kubernetes/pkg/acp/oidc"
	hubv1alpha1 "github.com/traefik/hub-agent-kubernetes/pkg/crd/api/hub/v1alpha1"
)

// Config is the configuration of an Access Control Policy. It is used to setup ACP handlers.
type Config struct {
	JWT       *jwt.Config
	BasicAuth *basicauth.Config
	OIDC      *oidc.Config
}

// ConfigFromPolicy returns an ACP configuration for the given policy.
func ConfigFromPolicy(policy *hubv1alpha1.AccessControlPolicy) *Config {
	switch {
	case policy.Spec.JWT != nil:
		jwtCfg := policy.Spec.JWT

		return &Config{
			JWT: &jwt.Config{
				SigningSecret:              jwtCfg.SigningSecret,
				SigningSecretBase64Encoded: jwtCfg.SigningSecretBase64Encoded,
				PublicKey:                  jwtCfg.PublicKey,
				JWKsFile:                   jwt.FileOrContent(jwtCfg.JWKsFile),
				JWKsURL:                    jwtCfg.JWKsURL,
				StripAuthorizationHeader:   jwtCfg.StripAuthorizationHeader,
				ForwardHeaders:             jwtCfg.ForwardHeaders,
				TokenQueryKey:              jwtCfg.TokenQueryKey,
				Claims:                     jwtCfg.Claims,
			},
		}

	case policy.Spec.BasicAuth != nil:
		basicCfg := policy.Spec.BasicAuth

		return &Config{
			BasicAuth: &basicauth.Config{
				Users:                    basicCfg.Users,
				Realm:                    basicCfg.Realm,
				StripAuthorizationHeader: basicCfg.StripAuthorizationHeader,
				ForwardUsernameHeader:    basicCfg.ForwardUsernameHeader,
			},
		}

	case policy.Spec.OIDC != nil:
		oidcCfg := policy.Spec.OIDC
		var tls *oidc.TLS
		if oidcCfg.TLS != nil {
			tls = &oidc.TLS{
				CABundle:           oidcCfg.TLS.CABundle,
				InsecureSkipVerify: oidcCfg.TLS.InsecureSkipVerify,
			}
		}

		return &Config{
			OIDC: &oidc.Config{
				Issuer:       oidcCfg.Issuer,
				ClientID:     oidcCfg.ClientID,
				ClientSecret: oidcCfg.ClientSecret,
				TLS:          tls,
				RedirectURL:  oidcCfg.RedirectURL,
				LogoutURL:    oidcCfg.LogoutURL,
				Scopes:       oidcCfg.Scopes,
				AuthParams:   oidcCfg.AuthParams,
				StateCookie: &oidc.AuthStateCookie{
					Secret:   oidcCfg.StateCookie.Secret,
					Path:     oidcCfg.StateCookie.Path,
					Domain:   oidcCfg.StateCookie.Domain,
					SameSite: oidcCfg.StateCookie.SameSite,
					Secure:   oidcCfg.StateCookie.Secure,
				},
				Session: &oidc.AuthSession{
					Secret:   oidcCfg.Session.Secret,
					Path:     oidcCfg.Session.Path,
					Domain:   oidcCfg.Session.Domain,
					SameSite: oidcCfg.Session.SameSite,
					Secure:   oidcCfg.Session.Secure,
					Refresh:  oidcCfg.Session.Refresh,
				},
				ForwardHeaders: oidcCfg.ForwardHeaders,
				Claims:         oidcCfg.Claims,
			},
		}
	default:
		return &Config{}
	}
}

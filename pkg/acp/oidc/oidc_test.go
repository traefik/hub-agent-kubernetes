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
	"crypto/aes"
	"crypto/tls"
	"encoding/base64"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	gooidc "github.com/coreos/go-oidc/v3/oidc"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
	"github.com/traefik/hub-agent-kubernetes/pkg/acp/jwt/expr"
	"golang.org/x/oauth2"
)

func TestNewMiddlewareFromSource_ValidatesConfiguration(t *testing.T) {
	tests := []struct {
		desc    string
		cfg     Config
		wantErr string
	}{
		{
			desc: "empty Issuer",
			cfg: Config{
				Issuer:       "",
				ClientID:     "bar",
				ClientSecret: "bat",
				RedirectURL:  "test",
				StateCookie: &AuthStateCookie{
					Secret: "secret1234567890",
				},
				Session: &AuthSession{
					Secret: "secret1234567890",
				},
			},
			wantErr: "validate configuration: missing issuer",
		},
		{
			desc: "empty ClientID",
			cfg: Config{
				Issuer:       "foo",
				ClientID:     "",
				ClientSecret: "bat",
				RedirectURL:  "test",
				StateCookie: &AuthStateCookie{
					Secret: "secret1234567890",
				},
				Session: &AuthSession{
					Secret: "secret1234567890",
				},
			},
			wantErr: "validate configuration: missing client ID",
		},
		{
			desc: "empty ClientSecret",
			cfg: Config{
				Issuer:       "foo",
				ClientID:     "bar",
				ClientSecret: "",
				RedirectURL:  "test",
				StateCookie: &AuthStateCookie{
					Secret: "secret1234567890",
				},
				Session: &AuthSession{
					Secret: "secret1234567890",
				},
			},
			wantErr: "validate configuration: missing client secret",
		},
		{
			desc: "empty Session Secret",
			cfg: Config{
				Issuer:       "foo",
				ClientID:     "bar",
				ClientSecret: "bat",
				RedirectURL:  "test",
				StateCookie: &AuthStateCookie{
					Secret: "secret1234567890",
				},
				Session: &AuthSession{
					Secret: "",
				},
			},
			wantErr: "validate configuration: missing session secret",
		},
		{
			desc: "bad size for Session Secret",
			cfg: Config{
				Issuer:       "foo",
				ClientID:     "bar",
				ClientSecret: "bat",
				RedirectURL:  "test",
				StateCookie: &AuthStateCookie{
					Secret: "secret1234567890",
				},
				Session: &AuthSession{
					Secret: "foo",
				},
			},
			wantErr: "validate configuration: session secret must be 16, 24 or 32 characters long",
		},
		{
			desc: "empty State Secret",
			cfg: Config{
				Issuer:       "foo",
				ClientID:     "bar",
				ClientSecret: "bat",
				RedirectURL:  "test",
				StateCookie:  &AuthStateCookie{},
				Session: &AuthSession{
					Secret: "secret1234567890",
				},
			},
			wantErr: "validate configuration: missing state secret",
		},
		{
			desc: "bad size for State Secret",
			cfg: Config{
				Issuer:       "foo",
				ClientID:     "bar",
				ClientSecret: "bat",
				RedirectURL:  "test",
				StateCookie: &AuthStateCookie{
					Secret: "foo",
				},
				Session: &AuthSession{
					Secret: "secret1234567890",
				},
			},
			wantErr: "validate configuration: state secret must be 16, 24 or 32 characters long",
		},
		{
			desc: "empty RedirectURL",
			cfg: Config{
				Issuer:       "foo",
				ClientID:     "bar",
				ClientSecret: "bar",
				RedirectURL:  "",
				StateCookie: &AuthStateCookie{
					Secret: "secret1234567890",
				},
				Session: &AuthSession{
					Secret: "secret1234567890",
				},
			},
			wantErr: "validate configuration: missing redirect URL",
		},
	}

	for _, test := range tests {
		test := test
		t.Run(test.desc, func(t *testing.T) {
			ApplyDefaultValues(&test.cfg)
			_, err := NewHandler(context.Background(), &test.cfg, test.desc)

			if test.wantErr != "" {
				assert.Error(t, err)
				assert.Equal(t, test.wantErr, err.Error())
				return
			}

			assert.NoError(t, err)
		})
	}
}

func TestNewMiddlewareFromSource_SelfSignedIssuer(t *testing.T) {
	certPem := []byte(`-----BEGIN CERTIFICATE-----
MIIB/DCCAaGgAwIBAgIRAK5Wtyw1YesDMV3koA8fJsswCgYIKoZIzj0EAwIwLDET
MBEGA1UEChMKQ29udGFpbm91czEVMBMGA1UEAxMMVHJhZWZpa0VFIENBMCAXDTE5
MTIwNDA2NTIwMloYDzIxMTkxMTEwMDY1MjAyWjAWMRQwEgYDVQQDEwtzZXJ2ZXIu
dGVzdDBZMBMGByqGSM49AgEGCCqGSM49AwEHA0IABLE9Ao3QpZNz5DzEnYHxz/Ot
3xyBNRoFndrF5FrcpyLFg/Zi4tl82abnlr+eSN4kStD8lxFU5dEq79cJljDHQzaj
gbcwgbQwDgYDVR0PAQH/BAQDAgGGMBMGA1UdJQQMMAoGCCsGAQUFBwMBMAwGA1Ud
EwEB/wQCMAAwKQYDVR0OBCIEIFy7RoCVcFDDZ3gtoPfrAcsUBw3HS6hSw7RiiCGF
Xkm+MCsGA1UdIwQkMCKAIPOHvhXw6cD5Kx9NdmEwmGCGdWCH1lI75OVP9/Qqvpdm
MCcGA1UdEQQgMB6CC3NlcnZlci50ZXN0gglsb2NhbGhvc3SHBH8AAAEwCgYIKoZI
zj0EAwIDSQAwRgIhAOnI/7c0cv0QakZ7c/e8ijCNH5sG/2p4JbtsEDadlNvgAiEA
wFlfYEb6TyjHQfXIZecpdKdmuB8Jm4SZIkDrFH1SlEw=
-----END CERTIFICATE-----`)

	keyPem := []byte(`-----BEGIN EC PRIVATE KEY-----
MHcCAQEEIEf/bAjKUPuGEon0LU66l1Hk57SnZp2kA42cioePzzsdoAoGCCqGSM49
AwEHoUQDQgAEsT0CjdClk3PkPMSdgfHP863fHIE1GgWd2sXkWtynIsWD9mLi2XzZ
pueWv55I3iRK0PyXEVTl0Srv1wmWMMdDNg==
-----END EC PRIVATE KEY-----`)

	srv := httptest.NewUnstartedServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		require.Equal(t, "/.well-known/openid-configuration", r.RequestURI)

		w.Header().Set("Content-Type", "application/json")
		_, err := w.Write([]byte(`{"issuer": "https://` + r.Host + `"}`))
		require.NoError(t, err)
	}))

	cert, err := tls.X509KeyPair(certPem, keyPem)
	require.NoError(t, err)

	srv.TLS = &tls.Config{Certificates: []tls.Certificate{cert}}
	srv.StartTLS()

	cfg := Config{
		Issuer:       srv.URL,
		ClientID:     "client-id",
		ClientSecret: "client-secret",
		TLS: &TLS{
			CABundle: certPem,
		},
		RedirectURL: "test",
		StateCookie: &AuthStateCookie{
			Secret: "secret1234567890",
		},
		Session: &AuthSession{
			Secret: "secret1234567890",
		},
	}
	ApplyDefaultValues(&cfg)

	_, err = NewHandler(context.Background(), &cfg, "oidc")
	require.NoError(t, err)
}

func TestMiddleware_RedirectsCorrectly(t *testing.T) {
	tests := []struct {
		desc    string
		request *http.Request
		cfg     Config

		wantStatus      int
		wantRedirect    bool
		wantRedirectURL string
		wantParams      map[string]string
		wantCookies     map[string]*http.Cookie
	}{
		{
			desc:    "redirects with absolute redirect URL",
			request: httptest.NewRequest(http.MethodGet, "/foo", nil),
			cfg: Config{
				RedirectURL: "http://example.com/callback",
				AuthParams: map[string]string{
					"hd": "example.com",
				},
				StateCookie: &AuthStateCookie{
					Secret: "secret1234567890",
					Path:   "/",
				},
			},
			wantStatus:      http.StatusFound,
			wantRedirect:    true,
			wantRedirectURL: "http://example.com/callback",
			wantParams: map[string]string{
				"hd": "example.com",
			},
		},
		{
			desc:    "redirects with relative redirect URL",
			request: httptest.NewRequest(http.MethodGet, "http://blah.meh/foo", nil),
			cfg: Config{
				RedirectURL: "/callback",
				StateCookie: &AuthStateCookie{
					Secret: "secret1234567890",
					Path:   "/",
				},
			},
			wantStatus:      http.StatusFound,
			wantRedirect:    true,
			wantRedirectURL: "http://blah.meh/callback",
		},
		{
			desc:    "redirects with relative redirect scheme",
			request: httptest.NewRequest(http.MethodGet, "https://blah.meh/foo", nil),
			cfg: Config{
				RedirectURL: "example.com/callback",
				StateCookie: &AuthStateCookie{
					Secret: "secret1234567890",
					Path:   "/",
				},
			},
			wantStatus:      http.StatusFound,
			wantRedirect:    true,
			wantRedirectURL: "https://example.com/callback",
		},
		{
			desc:    "returns unauthorized if method is PUT",
			request: httptest.NewRequest(http.MethodPut, "/foo", nil),
			cfg: Config{
				StateCookie: &AuthStateCookie{
					Secret: "secret1234567890",
				},
			},
			wantStatus: http.StatusUnauthorized,
		},
		{
			desc:    "returns unauthorized if method is POST",
			request: httptest.NewRequest(http.MethodPost, "/foo", nil),
			cfg: Config{
				StateCookie: &AuthStateCookie{
					Secret: "secret1234567890",
				},
			},
			wantStatus: http.StatusUnauthorized,
		},
		{
			desc:    "returns unauthorized if method is DELETE",
			request: httptest.NewRequest(http.MethodDelete, "/foo", nil),
			cfg: Config{
				StateCookie: &AuthStateCookie{
					Secret: "secret1234567890",
				},
			},
			wantStatus: http.StatusUnauthorized,
		},
		{
			desc:    "returns unauthorized if method is PATCH",
			request: httptest.NewRequest(http.MethodPatch, "/foo", nil),
			cfg: Config{
				StateCookie: &AuthStateCookie{
					Secret: "secret1234567890",
				},
			},
			wantStatus: http.StatusUnauthorized,
		},
		{
			desc:    "returns unauthorized if path is favicon.ico",
			request: httptest.NewRequest(http.MethodGet, "https://foo.com/favicon.ico", nil),
			cfg: Config{
				StateCookie: &AuthStateCookie{
					Secret: "secret1234567890",
				},
			},
			wantStatus: http.StatusUnauthorized,
		},
		{
			desc:    "redirects with custom state cookie domain",
			request: httptest.NewRequest(http.MethodGet, "/foo", nil),
			cfg: Config{
				RedirectURL: "http://example.com/callback",
				AuthParams: map[string]string{
					"hd": "example.com",
				},
				StateCookie: &AuthStateCookie{
					Secret: "secret1234567890",
					Path:   "/",
					Domain: "example.com",
				},
			},
			wantStatus:      http.StatusFound,
			wantRedirect:    true,
			wantRedirectURL: "http://example.com/callback",
			wantParams: map[string]string{
				"hd": "example.com",
			},
			wantCookies: map[string]*http.Cookie{
				"test-state": {
					Name:     "test-state",
					Path:     "/",
					Domain:   "example.com",
					SameSite: http.SameSiteLaxMode,
					MaxAge:   600,
					HttpOnly: true,
				},
			},
		},
	}

	for _, test := range tests {
		test := test
		t.Run(test.desc, func(t *testing.T) {
			ApplyDefaultValues(&test.cfg)

			oauth := &oauth2.Config{
				Endpoint: oauth2.Endpoint{
					AuthURL: "http://foobar.com",
				},
			}

			session := newSessionStoreMock(t).
				OnGetRaw(mock.Anything).TypedReturns(nil, nil).Once().
				Parent

			handler := buildHandler(t)
			handler.oauth = oauth
			handler.session = session
			handler.cfg = &test.cfg

			test.request.Header.Add("X-Forwarded-Method", test.request.Method)
			test.request.Header.Add("X-Forwarded-Proto", "http")
			test.request.Header.Add("X-Forwarded-Host", test.request.Host)
			test.request.Header.Add("X-Forwarded-URI", test.request.URL.RequestURI())

			w := httptest.NewRecorder()
			handler.ServeHTTP(w, test.request)

			assert.Equal(t, test.wantStatus, w.Code)

			if test.wantRedirect {
				require.NotEmpty(t, w.Header().Get("location"))
				u, err := url.Parse(w.Header().Get("location"))
				require.NoError(t, err)
				assert.Equal(t, test.wantRedirectURL, u.Query().Get("redirect_uri"))

				if test.wantParams != nil {
					for k, v := range test.wantParams {
						assert.Equal(t, v, u.Query().Get(k))
					}
				}

				if test.wantCookies != nil {
					resultCookies := map[string]*http.Cookie{}
					for _, c := range w.Result().Cookies() {
						resultCookies[c.Name] = c
					}
					for name, want := range test.wantCookies {
						require.NotEmpty(t, resultCookies[name])
						// Here we don't care about the calculated value
						want.Value = resultCookies[name].Value
						assert.Equal(t, want.String(), resultCookies[name].String())
					}
				}
			}
		})
	}
}

func boolPtr(b bool) *bool {
	return &b
}

func TestMiddleware_ExchangesTokenOnCallback(t *testing.T) {
	cfg := Config{
		Issuer:       "http://foo.com",
		ClientID:     "client-id",
		ClientSecret: "client-secret",
		RedirectURL:  "http://foobar.com/callback",
		StateCookie: &AuthStateCookie{
			Secret:   "secret1234567890",
			Path:     "/",
			SameSite: "lax",
			Secure:   true,
		},
		Session: &AuthSession{Refresh: boolPtr(false)},
	}

	oauth2tok := &oauth2.Token{
		AccessToken: "access-token",
		TokenType:   "bearer",
	}

	oauth2tok = oauth2tok.WithExtra(map[string]interface{}{"id_token": jwtToken})

	oauth := newOAuthProviderMock(t).
		OnExchangeRaw(mock.Anything, mock.Anything).TypedReturns(oauth2tok, nil).Once().
		Parent

	wantSession := SessionData{
		AccessToken: oauth2tok.AccessToken,
		IDToken:     jwtToken,
		TokenType:   oauth2tok.TokenType,
	}

	session := newSessionStoreMock(t).
		OnGetRaw(mock.Anything).TypedReturns(nil, nil).Once().
		OnCreateRaw(mock.Anything, wantSession).TypedReturns(nil).Once().
		Parent

	handler := buildHandler(t)
	handler.oauth = oauth
	handler.session = session
	handler.cfg = &cfg

	state := StateData{
		RedirectID: "aaaaa",
		Nonce:      "n-0S6_WzA2Mj",
		OriginURL:  "http://app.bar.com",
	}

	stateCookie, err := handler.newStateCookie(state)
	require.NoError(t, err)

	w := httptest.NewRecorder()

	r := httptest.NewRequest(http.MethodGet, "http://foobar.com/callback?state=aaaaa", nil)
	r.Header.Set("X-Forwarded-Method", r.Method)
	r.Header.Set("X-Forwarded-Proto", "http")
	r.Header.Set("X-Forwarded-Host", r.Host)
	r.Header.Set("X-Forwarded-URI", r.URL.RequestURI())
	r.AddCookie(stateCookie)

	handler.ServeHTTP(w, r)

	assert.Equal(t, http.StatusFound, w.Code)
	assert.Equal(t, state.OriginURL, w.Header().Get("location"))
	assert.Equal(t, "test-state=; Path=/; Max-Age=0", w.Header().Get("Set-Cookie"))
}

func TestMiddleware_ForwardsCorrectly(t *testing.T) {
	tests := []struct {
		desc    string
		cfg     Config
		expiry  time.Time
		idToken string
		headers map[string]string

		wantStatus              int
		wantNextCalled          bool
		wantUpdateSessionCalled bool
		wantForwardedHeaders    map[string]string
	}{
		{
			desc: "returns bad request if the stored id token is bad",
			cfg: Config{
				Issuer:       "http://foo.com",
				ClientID:     "clientID",
				ClientSecret: "secret1234567890",
				RedirectURL:  "http://foo.com",
				Session: &AuthSession{
					Secret: "secret1234567890",
				},
				StateCookie: &AuthStateCookie{
					Secret: "secret1234567890",
				},
			},
			idToken:    "badtoken, very bad token.",
			wantStatus: http.StatusBadRequest,
		},
		{
			desc: "returns forbidden if the claims are not valid",
			cfg: Config{
				Issuer:       "http://foo.com",
				ClientID:     "clientID",
				ClientSecret: "secret1234567890",
				RedirectURL:  "http://foo.com",
				Claims:       "Equals(`group`,`dev`)",
				Session: &AuthSession{
					Secret: "secret1234567890",
				},
				StateCookie: &AuthStateCookie{
					Secret: "secret1234567890",
				},
			},
			idToken:    jwtToken,
			wantStatus: http.StatusForbidden,
		},
		{
			desc: "refreshes token if expired",
			cfg: Config{
				Issuer:       "http://foo.com",
				ClientID:     "clientID",
				ClientSecret: "secret1234567890",
				RedirectURL:  "http://foo.com",
				Claims:       "Equals(`group`,`admin`)",
				Session: &AuthSession{
					Secret: "secret1234567890",
				},
				StateCookie: &AuthStateCookie{
					Secret: "secret1234567890",
				},
				ForwardHeaders: map[string]string{
					"X-App-Group": "group",
				},
			},
			expiry:                  time.Now().Add(-1 * time.Minute),
			idToken:                 jwtToken,
			wantStatus:              http.StatusOK,
			wantNextCalled:          true,
			wantUpdateSessionCalled: true,
			wantForwardedHeaders: map[string]string{
				"X-App-Group":   "admin",
				"Authorization": "Bearer refreshed-token",
			},
		},
		{
			desc: "forwards call (and header is canonicalized)",
			cfg: Config{
				Issuer:       "http://foo.com",
				ClientID:     "clientID",
				ClientSecret: "secret1234567890",
				RedirectURL:  "http://foo.com",
				Claims:       "Equals(`group`,`admin`)",
				Session: &AuthSession{
					Secret: "secret1234567890",
				},
				StateCookie: &AuthStateCookie{
					Secret: "secret1234567890",
				},
				ForwardHeaders: map[string]string{
					"x-App-Group": "group",
				},
			},
			idToken:        jwtToken,
			wantStatus:     http.StatusOK,
			wantNextCalled: true,
			wantForwardedHeaders: map[string]string{
				"X-App-Group":   "admin",
				"Authorization": "Bearer test",
			},
		},
		{
			desc: "overwrite forwarded headers",
			cfg: Config{
				Issuer:       "http://foo.com",
				ClientID:     "clientID",
				ClientSecret: "secret1234567890",
				RedirectURL:  "http://foo.com",
				Claims:       "Equals(`group`,`admin`)",
				Session: &AuthSession{
					Secret: "secret1234567890",
				},
				StateCookie: &AuthStateCookie{
					Secret: "secret1234567890",
				},
				ForwardHeaders: map[string]string{
					"x-App-Group": "group",
				},
			},
			idToken: jwtToken,
			headers: map[string]string{
				"x-App-Group":   "supergroup",
				"Authorization": "Basic foo",
			},
			wantStatus:     http.StatusOK,
			wantNextCalled: true,
			wantForwardedHeaders: map[string]string{
				"X-App-Group":   "admin",
				"Authorization": "Bearer test",
			},
		},
	}

	for _, test := range tests {
		test := test
		t.Run(test.desc, func(t *testing.T) {
			ApplyDefaultValues(&test.cfg)

			session := newSessionStoreMock(t).
				OnGetRaw(mock.Anything).ReturnsFn(func(*http.Request) (*SessionData, error) {
				expiry := test.expiry
				if expiry.IsZero() {
					expiry = time.Now().Add(time.Minute)
				}

				return &SessionData{
					AccessToken: "test",
					IDToken:     test.idToken,
					Expiry:      expiry,
				}, nil
			}).Once().
				Parent

			if test.wantUpdateSessionCalled {
				session.OnUpdateRaw(mock.Anything, mock.Anything, mock.Anything).TypedReturns(nil).Once()
			}

			if test.wantStatus == http.StatusOK {
				session.OnRemoveCookieRaw(mock.Anything).Once()
			}

			oauth := newOAuthProviderMock(t).
				OnTokenSourceRaw(mock.Anything).ReturnsFn(func(*oauth2.Token) oauth2.TokenSource {
				tok := &oauth2.Token{
					AccessToken:  "refreshed-token",
					TokenType:    "test2",
					RefreshToken: "test2",
					Expiry:       time.Now(),
				}

				tok = tok.WithExtra(map[string]interface{}{"id_token": jwtToken})

				return tokenSourceMock{token: tok}
			}).Maybe().
				Parent

			pred, _ := expr.Parse(test.cfg.Claims)

			handler := buildHandler(t)
			handler.oauth = oauth
			handler.session = session
			handler.validateClaims = pred
			handler.cfg = &test.cfg

			r := httptest.NewRequest(http.MethodGet, "/foo", nil)
			for k, v := range test.headers {
				r.Header.Add(k, v)
			}
			w := httptest.NewRecorder()

			handler.ServeHTTP(w, r)

			assert.Equal(t, test.wantStatus, w.Code)
			for hdrdesc, hdrValue := range test.wantForwardedHeaders {
				assert.Equal(t, hdrValue, r.Header.Get(hdrdesc))
			}
		})
	}
}

func TestMiddleware_LogsOutCorrectly(t *testing.T) {
	tests := []struct {
		desc      string
		logoutURL string
	}{
		{
			desc:      "logout URL is a path",
			logoutURL: "/logout",
		},
		{
			desc:      "logout URL is a host and path",
			logoutURL: "example.com/logout",
		},
	}

	for _, test := range tests {
		test := test
		t.Run(test.desc, func(t *testing.T) {
			session := newSessionStoreMock(t).
				OnDeleteRaw(mock.Anything, mock.Anything).TypedReturns(nil).Once().
				Parent

			cfg := Config{
				Issuer:       "http://foo.com",
				ClientID:     "clientID",
				ClientSecret: "secret1234567890",
				RedirectURL:  "http://foo.com",
				StateCookie:  &AuthStateCookie{Secret: "secret1234567890"},
				Session:      &AuthSession{Secret: "secret1234567890"},
				LogoutURL:    test.logoutURL,
			}

			handler := buildHandler(t)
			handler.cfg = &cfg
			handler.session = session

			r := httptest.NewRequest(http.MethodDelete, "https://example.com/logout", nil)
			r.Header.Add("X-Forwarded-Method", r.Method)
			r.Header.Add("X-Forwarded-Proto", "http")
			r.Header.Add("X-Forwarded-Host", r.Host)
			r.Header.Add("X-Forwarded-URI", r.URL.RequestURI())

			w := httptest.NewRecorder()
			handler.ServeHTTP(w, r)

			assert.Equal(t, http.StatusNoContent, w.Code)
		})
	}
}

func buildHandler(t *testing.T) *Handler {
	t.Helper()

	keySet := func(jwt string) ([]byte, error) { return parseJwt(t, jwt) }
	verifier := gooidc.NewVerifier(
		"https://openid.c2id.com",
		keySetMock(keySet),
		&gooidc.Config{
			ClientID:             "client-12345",
			SkipExpiryCheck:      true,
			SkipIssuerCheck:      true,
			SupportedSigningAlgs: []string{"ES256"},
		},
	)

	stateBlock, err := aes.NewCipher([]byte("secret1234567890"))
	require.NoError(t, err)

	client, err := newHTTPClient(nil)
	require.NoError(t, err)

	return &Handler{
		name:       "test",
		stateBlock: stateBlock,
		rand:       newRandom(),
		client:     client,
		verifier:   verifier,
	}
}

type tokenSourceMock struct {
	token *oauth2.Token
	err   error
}

func (t tokenSourceMock) Token() (*oauth2.Token, error) {
	return t.token, t.err
}

type keySetMock func(string) ([]byte, error)

func (k keySetMock) VerifySignature(_ context.Context, jwt string) ([]byte, error) {
	return k(jwt)
}

const jwtToken = `eyJhbGciOiJFUzI1NiIsInR5cCI6IkpXVCJ9.eyJzdWIiOiJhbGljZSIsImlzcyI6Imh0dHBzOi8vb3BlbmlkLmMyaWQuY29tIiwiYXVkIjoiY2xpZW50LTEyMzQ1Iiwibm9uY2UiOiJuLTBTNl9XekEyTWoiLCJhdXRoX3RpbWUiOjEzMTEyODA5NjksImFjciI6ImMyaWQubG9hLmhpc2VjIiwiZ3JvdXAiOiJhZG1pbiIsImlhdCI6MTUxNjIzOTAyMn0.EVA0Ec03xmOfCpJGng8dvMe7OoN6LLUX84f5qL0hircxs03lmZhc2UXu3Ipb6QndtVU5AZBxZkWtvGs2Ls3RuA`

func parseJwt(t *testing.T, raw string) ([]byte, error) {
	t.Helper()

	sp := strings.Split(raw, ".")

	data := make([]byte, base64.RawURLEncoding.DecodedLen(len(sp[1])))

	_, err := base64.RawURLEncoding.Decode(data, []byte(sp[1]))
	require.NoError(t, err)

	return data, nil
}

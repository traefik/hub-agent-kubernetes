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
	"crypto/cipher"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/coreos/go-oidc/v3/oidc"
	"github.com/rs/zerolog/log"
	"github.com/traefik/hub-agent-kubernetes/pkg/acp/jwt/expr"
	"golang.org/x/oauth2"
)

const maxCookieSize = 4000

// OAuthProvider represents a structure that can interface with an OAuth provider.
type OAuthProvider interface {
	AuthCodeURL(string, ...oauth2.AuthCodeOption) string
	Exchange(context.Context, string, ...oauth2.AuthCodeOption) (*oauth2.Token, error)
	TokenSource(ctx context.Context, t *oauth2.Token) oauth2.TokenSource
}

// IDTokenVerifier represents a type that can verify an ID token.
type IDTokenVerifier interface {
	Verify(context.Context, string) (*oidc.IDToken, error)
}

// StateData is the initial data captured at redirect time.
// See https://openid.net/specs/openid-connect-core-1_0.html#AuthRequest
type StateData struct {
	// RedirectID is used to prevent CSRF and XSRF attacks.
	RedirectID string
	// Nonce is used to mitigate replay attacks.
	Nonce string
	// OriginURL is the actual resource initially requested by the client.
	OriginURL string
	// CodeVerifier is used to generate code challenges when using PKCE.
	// It is only set when using PKCE.
	CodeVerifier string
}

// SessionData is the state of the session.
type SessionData struct {
	AccessToken  string
	TokenType    string
	RefreshToken string
	IDToken      string

	// Expiry is the expiration time of the access token.
	Expiry time.Time
}

// IsExpired determines if the current access token is expired.
func (d SessionData) IsExpired() bool {
	return d.Expiry.Before(time.Now())
}

// ToToken returns an OAuth2 Token from the session data.
func (d SessionData) ToToken() *oauth2.Token {
	return &oauth2.Token{
		AccessToken:  d.AccessToken,
		TokenType:    d.TokenType,
		RefreshToken: d.RefreshToken,
		Expiry:       d.Expiry,
	}
}

// SessionStore represents a type that can manage a session for a given request.
type SessionStore interface {
	Create(http.ResponseWriter, SessionData) error
	Update(http.ResponseWriter, *http.Request, SessionData) error
	Delete(http.ResponseWriter, *http.Request) error
	Get(*http.Request) (*SessionData, error)
	RemoveCookie(*http.Request)
}

// Handler performs OIDC authentication and authorisation on incoming requests.
type Handler struct {
	name string
	rand random

	verifier   IDTokenVerifier
	oauth      OAuthProvider
	session    SessionStore
	stateBlock cipher.Block

	validateClaims expr.Predicate

	client *http.Client

	cfg *Config
}

// NewHandler creates a new instance of a Handler from an auth source.
func NewHandler(ctx context.Context, cfg *Config, name string) (*Handler, error) {
	if err := cfg.Validate(); err != nil {
		return nil, fmt.Errorf("validate configuration: %w", err)
	}

	client, err := newHTTPClient(cfg.TLS)
	if err != nil {
		return nil, fmt.Errorf("create HTTP client: %w", err)
	}

	provider, err := BuildProvider(oidc.ClientContext(ctx, client), cfg)
	if err != nil {
		return nil, err
	}

	verifier := provider.Verifier(&oidc.Config{ClientID: cfg.ClientID})
	oauth := &oauth2.Config{
		ClientID:     cfg.ClientID,
		ClientSecret: cfg.ClientSecret,
		Endpoint:     provider.Endpoint(),
		Scopes:       cfg.Scopes,
	}

	var pred expr.Predicate
	if cfg.Claims != "" {
		pred, err = expr.Parse(cfg.Claims)
		if err != nil {
			return nil, fmt.Errorf("unable to make predicate: %w", err)
		}
	}

	block, err := aes.NewCipher([]byte(cfg.StateCookie.Secret))
	if err != nil {
		return nil, fmt.Errorf("new cipher: %w", err)
	}

	sess, err := NewCookieSessionStore(name+"-session", cfg.Session, newRandom(), maxCookieSize)
	if err != nil {
		return nil, fmt.Errorf("new cookie store: %w", err)
	}

	return &Handler{
		name:           name,
		cfg:            cfg,
		verifier:       verifier,
		oauth:          oauth,
		rand:           newRandom(),
		session:        sess,
		stateBlock:     block,
		validateClaims: pred,
		client:         client,
	}, nil
}

// The implementation below should be compliant with the Authorization Code Flow
// of the specification at
// https://openid.net/specs/openid-connect-core-1_0.html#CodeFlowAuth , which is
// what we refer to in the comments all along.
// It should also correspond to the diagram at
// https://doc.traefik.io/traefik-enterprise/assets/img/oidc-middleware-diagram.png
// which we refer to as "the diagram" in the following.
// The "actors" in this flow are the browser/user (== the user-agent in the
// spec), the here middleware (== the client), and the configured Authentication
// source (== the server, aka the openid connect provider in the diagram).

// ServeHTTP handles an incoming http request.
func (h *Handler) ServeHTTP(rw http.ResponseWriter, req *http.Request) {
	// We add the configured http.Client to the request context,
	// to use it in the OAuth2 and OIDC libraries.
	l := log.With().Str("handler_type", "OIDC").Str("handler_name", h.name).Logger()

	logoutURL := resolveURL(req, h.cfg.LogoutURL)
	forwardedURL := fmt.Sprintf("%s://%s%s", req.Header.Get("X-Forwarded-Proto"), req.Header.Get("X-Forwarded-Host"), req.Header.Get("X-Forwarded-Uri"))
	forwardedMethod := req.Header.Get("X-Forwarded-Method")

	if isURL(forwardedURL, logoutURL) && forwardedMethod == http.MethodDelete {
		if err := h.session.Delete(rw, req); err != nil {
			l.Debug().Err(err).Msg("Unable to delete the session")
		}

		rw.WriteHeader(http.StatusNoContent)

		return
	}

	sess, err := h.session.Get(req)
	if err != nil {
		l.Debug().Err(err).Msg("Unable to get the session")
		http.Error(rw, http.StatusText(http.StatusUnauthorized), http.StatusUnauthorized)

		return
	}

	// We get in here either because we're in the initial run (no session yet),
	// or if we have an expired session, but session refreshing is disabled by
	// configuration. For the gritty details, it means we don't need to refresh tokens (so
	// we won't ask for them), so we don't need to be in offline access, and we don't
	// need the (user consent) prompt after asking for credentials.
	if sess == nil || (sess.IsExpired() && !(*h.cfg.Session.Refresh)) {
		redirectURL := resolveURL(req, h.cfg.RedirectURL)

		if isURL(forwardedURL, redirectURL) {
			l.Debug().Msg("Handle provider callback")
			// 5th step of the diagram, we're handling the redirected response from the auth server.
			// spec: receiving response of section 3.1.2.5
			h.handleProviderCallback(rw, req, redirectURL)

			return
		}

		if !h.shouldRedirect(req) {
			l.Debug().Msg("Received a request that should not be redirected")
			http.Error(rw, http.StatusText(http.StatusUnauthorized), http.StatusUnauthorized)

			return
		}

		// 1st step of diagram, i.e. the (unauthenticated) request is coming from the user.
		h.redirectToProvider(rw, req, redirectURL)

		return
	}

	var refreshSession bool
	sess, refreshSession, err = h.maybeRefreshSession(req.Context(), sess)
	if err != nil {
		l.Debug().Err(err).Msg("Unable to refresh the session")

		if err = h.session.Delete(rw, req); err != nil {
			l.Debug().Err(err).Msg("Unable to delete the session")
		}

		if !h.shouldRedirect(req) {
			l.Debug().Err(err).Msg("Received a request that should not be redirected")
			http.Error(rw, http.StatusText(http.StatusUnauthorized), http.StatusUnauthorized)

			return
		}

		// 1st step of diagram, restart from scratch, as if initial request.
		redirectURL := resolveURL(req, h.cfg.RedirectURL)
		h.redirectToProvider(rw, req, redirectURL)

		return
	}

	// 9th step of diagram.
	var idToken *oidc.IDToken
	idToken, err = h.verifier.Verify(req.Context(), sess.IDToken)
	if err != nil {
		l.Debug().Err(err).Msg("Invalid ID token")
		http.Error(rw, http.StatusText(http.StatusBadRequest), http.StatusBadRequest)

		return
	}

	if refreshSession {
		if err = h.session.Update(rw, req, *sess); err != nil {
			l.Debug().Err(err).Msg("Unable to refresh the session")
			http.Error(rw, http.StatusText(http.StatusInternalServerError), http.StatusInternalServerError)

			return
		}
	}

	claims := make(map[string]interface{})
	if err = idToken.Claims(&claims); err != nil {
		l.Debug().Err(err).Msg("Unable to unmarshal claims")
		http.Error(rw, http.StatusText(http.StatusInternalServerError), http.StatusInternalServerError)

		return
	}

	if h.validateClaims != nil && !h.validateClaims(claims) {
		l.Debug().Err(err).Msg("Unauthorized claim")
		http.Error(rw, http.StatusText(http.StatusForbidden), http.StatusForbidden)

		return
	}

	if err = h.forwardHeader(req, claims); err != nil {
		l.Error().Err(err).Msg("Unable to set forwarded header")
		http.Error(rw, http.StatusText(http.StatusInternalServerError), http.StatusInternalServerError)

		return
	}

	// 10th step of diagram.
	req.Header.Set("Authorization", "Bearer "+sess.AccessToken)
	h.session.RemoveCookie(req)

	rw.WriteHeader(http.StatusOK)
}

func (h *Handler) forwardHeader(r *http.Request, claims map[string]interface{}) error {
	hdrs, err := expr.PluckClaims(h.cfg.ForwardHeaders, claims)
	if err != nil {
		return errors.New("unable to extract data from claims")
	}

	for name, vals := range hdrs {
		r.Header.Del(name)
		for _, val := range vals {
			r.Header.Add(name, val)
		}
	}

	return nil
}

func (h *Handler) maybeRefreshSession(ctx context.Context, sess *SessionData) (s *SessionData, refresh bool, err error) {
	if !(*h.cfg.Session.Refresh) || !sess.IsExpired() {
		return sess, false, nil
	}

	// We are in refresh mode and have and expired token, exchange for a new one.
	// (not shown on diagram).
	// spec: section 12.
	ts := h.oauth.TokenSource(ctx, sess.ToToken())
	tok, err := ts.Token()
	if err != nil {
		return nil, false, err
	}

	rawIDToken, ok := tok.Extra("id_token").(string)
	if !ok {
		return nil, false, errors.New("ID token not found")
	}

	sess = &SessionData{
		AccessToken:  tok.AccessToken,
		TokenType:    tok.TokenType,
		RefreshToken: tok.RefreshToken,
		IDToken:      rawIDToken,
		Expiry:       tok.Expiry,
	}
	return sess, true, nil
}

func (h *Handler) redirectToProvider(rw http.ResponseWriter, req *http.Request, redirectURL string) {
	l := log.With().Str("handler_type", "OIDC").Str("handler_name", h.name).Logger()
	originalURL := fmt.Sprintf("%s://%s%s", req.Header.Get("X-Forwarded-Proto"), req.Header.Get("X-Forwarded-Host"), req.Header.Get("X-Forwarded-Uri"))

	l.Debug().Msg("Set OriginURL in state: " + originalURL)

	state := StateData{
		RedirectID: h.rand.String(20),
		Nonce:      h.rand.String(20),
		OriginURL:  originalURL,
	}

	stateCookie, err := h.newStateCookie(state)
	if err != nil {
		l.Debug().Err(err).Msg("Unable to create state cookie")
		http.Error(rw, http.StatusText(http.StatusInternalServerError), http.StatusInternalServerError)

		return
	}

	l.Debug().Msg("Create State")
	http.SetCookie(rw, stateCookie)

	opts := []oauth2.AuthCodeOption{
		oauth2.SetAuthURLParam("redirect_uri", redirectURL),
		oidc.Nonce(state.Nonce),
	}

	if *h.cfg.Session.Refresh {
		// We want a refresh token in the response, which requires AccessTypeOffline,
		// which in turn requires consent prompt.
		// spec: section 11.
		opts = append(opts, oauth2.AccessTypeOffline, oauth2.SetAuthURLParam("prompt", "consent"))
	}

	for k, v := range h.cfg.AuthParams {
		opts = append(opts, oauth2.SetAuthURLParam(k, v))
	}

	// 2nd step of diagram.
	// which leads to 3rd step of diagram:
	// makes the browser send an /authorize to the auth server.
	// spec: section 3.1.2.1.
	http.Redirect(
		rw,
		req,
		h.oauth.AuthCodeURL(
			state.RedirectID,
			opts...,
		),
		http.StatusFound,
	)
}

func (h *Handler) handleProviderCallback(rw http.ResponseWriter, req *http.Request, redirectURL string) {
	l := log.With().Str("handler_type", "OIDC").Str("handler_name", h.name).Logger()

	state, err := h.getStateCookie(req)
	if err != nil {
		l.Debug().Err(err).Msg("Malformed state payload")
		http.Error(rw, http.StatusText(http.StatusBadRequest), http.StatusBadRequest)
		return
	}

	u, err := url.Parse(req.Header.Get("X-Forwarded-Uri"))
	if err != nil {
		l.Debug().Err(err).Msg("Malformed request ID")
	}

	if state == nil || u.Query().Get("state") != state.RedirectID {
		l.Debug().Err(err).Msg("Mismatched request ID or empty state")
		http.Error(rw, http.StatusText(http.StatusBadRequest), http.StatusBadRequest)
		return
	}

	opts := []oauth2.AuthCodeOption{
		oauth2.SetAuthURLParam("redirect_uri", redirectURL),
	}

	// 6th and 7th step of diagram.
	// spec: section 3.1.3.1.
	oauth2Token, err := h.oauth.Exchange(
		req.Context(),
		u.Query().Get("code"),
		opts...,
	)
	if err != nil {
		l.Debug().Err(err).Msg("Unable to exchange code")
		http.Error(rw, http.StatusText(http.StatusInternalServerError), http.StatusInternalServerError)
		return
	}

	// 7th step of diagram.
	// spec: section 3.1.3.3.
	rawIDToken, ok := oauth2Token.Extra("id_token").(string)
	if !ok {
		l.Debug().Err(err).Msg("ID token invalid or not found")
		http.Error(rw, http.StatusText(http.StatusInternalServerError), http.StatusInternalServerError)
		return
	}

	// spec: 3.1.3.7
	idToken, err := h.verifier.Verify(req.Context(), rawIDToken)
	if err != nil {
		l.Debug().Err(err).Msg("Invalid ID token")
		http.Error(rw, http.StatusText(http.StatusBadRequest), http.StatusBadRequest)
		return
	}

	// Nonce validation.
	if idToken.Nonce != state.Nonce {
		l.Debug().Err(err).Msg("Invalid Nonce")
		http.Error(rw, http.StatusText(http.StatusBadRequest), http.StatusBadRequest)
		return
	}

	// 8th step of diagram.
	sess := &SessionData{
		AccessToken:  oauth2Token.AccessToken,
		TokenType:    oauth2Token.TokenType,
		RefreshToken: oauth2Token.RefreshToken,
		IDToken:      rawIDToken,
		Expiry:       oauth2Token.Expiry,
	}
	if err = h.session.Create(rw, *sess); err != nil {
		l.Debug().Err(err).Msg("Unable to create session")
		http.Error(rw, http.StatusText(http.StatusInternalServerError), http.StatusInternalServerError)
		return
	}
	h.clearStateCookie(rw)

	// 8th step of diagram.
	http.Redirect(rw, req, state.OriginURL, http.StatusFound)
}

func (h *Handler) getStateCookie(r *http.Request) (*StateData, error) {
	stateCookie, ok := getCookie(r, h.name+"-state")
	if !ok {
		return nil, nil
	}

	var state StateData
	decoded := make([]byte, base64.RawURLEncoding.DecodedLen(len(stateCookie)))
	if _, err := base64.RawURLEncoding.Decode(decoded, stateCookie); err != nil {
		return nil, fmt.Errorf("decode state: %w", err)
	}

	blockSize := h.stateBlock.BlockSize()
	decrypted := make([]byte, len(decoded)-blockSize)
	iv := decoded[:blockSize]
	stream := cipher.NewCTR(h.stateBlock, iv)
	stream.XORKeyStream(decrypted, decoded[blockSize:])

	if err := json.Unmarshal(decrypted, &state); err != nil {
		return nil, fmt.Errorf("deserialize state: %w", err)
	}
	return &state, nil
}

func (h *Handler) newStateCookie(state StateData) (*http.Cookie, error) {
	statePayload, err := json.Marshal(state)
	if err != nil {
		return nil, fmt.Errorf("serialize state: %w", err)
	}

	blockSize := h.stateBlock.BlockSize()
	encrypted := make([]byte, blockSize+len(statePayload))
	iv := h.rand.Bytes(blockSize)
	copy(encrypted[:blockSize], iv)
	stream := cipher.NewCTR(h.stateBlock, iv)
	stream.XORKeyStream(encrypted[blockSize:], statePayload)

	return &http.Cookie{
		Name:     h.name + "-state",
		Value:    base64.RawURLEncoding.EncodeToString(encrypted),
		Path:     h.cfg.StateCookie.Path,
		MaxAge:   600,
		HttpOnly: true,
		SameSite: parseSameSite(h.cfg.StateCookie.SameSite),
		Secure:   h.cfg.StateCookie.Secure,
		Domain:   h.cfg.StateCookie.Domain,
	}, nil
}

func (h *Handler) clearStateCookie(w http.ResponseWriter) {
	http.SetCookie(w, &http.Cookie{
		Name:   h.name + "-state",
		Path:   "/",
		MaxAge: -1,
		Domain: h.cfg.StateCookie.Domain,
	})
}

func (h *Handler) shouldRedirect(req *http.Request) bool {
	forwardedMethod := req.Header.Get("X-Forwarded-Method")
	if forwardedMethod == http.MethodPost ||
		forwardedMethod == http.MethodDelete ||
		forwardedMethod == http.MethodPatch ||
		forwardedMethod == http.MethodPut {
		return false
	}

	// The favicon seems to do bad things, ban it.
	return !strings.Contains(req.Header.Get("X-Forwarded-Uri"), "favicon.ico")
}

func resolveURL(r *http.Request, u string) string {
	if u == "" {
		return u
	}

	if strings.HasPrefix(u, "http://") || strings.HasPrefix(u, "https://") {
		return u
	}

	scheme := "http"
	if r.TLS != nil {
		scheme = "https"
	}

	if u[0] == '/' {
		return scheme + "://" + r.Host + u
	}

	return scheme + "://" + u
}

func isURL(originalURL, otherURL string) bool {
	oURL, err := url.Parse(originalURL)
	if err != nil {
		return false
	}

	otURL, err := url.Parse(otherURL)
	if err != nil {
		return false
	}

	log.Debug().Msg("isURL: " + oURL.Host + "*" + otURL.Host + "*" + oURL.Path + "*" + otURL.Path)

	return oURL.Host == otURL.Host && oURL.Path == otURL.Path
}

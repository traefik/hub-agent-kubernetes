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
	"bytes"
	"crypto/aes"
	"crypto/cipher"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
)

const maxCookies = 180

// Randr represents an object that can return random bytes.
type Randr interface {
	Bytes(int) []byte
}

// CookieSessionStore stores and retrieve session information in given request cookies.
type CookieSessionStore struct {
	name    string
	cfg     *AuthSession
	maxSize int

	block cipher.Block
	rand  Randr
}

// NewCookieSessionStore creates a cookie session store.
func NewCookieSessionStore(name string, cfg *AuthSession, rand Randr, maxSize int) (*CookieSessionStore, error) {
	block, err := aes.NewCipher([]byte(cfg.Secret))
	if err != nil {
		return nil, err
	}

	return &CookieSessionStore{
		name:    name,
		cfg:     cfg,
		maxSize: maxSize,
		block:   block,
		rand:    rand,
	}, nil
}

// Create stores the session data into the request cookies.
func (s *CookieSessionStore) Create(w http.ResponseWriter, data SessionData) error {
	value, err := s.encode(data)
	if err != nil {
		return fmt.Errorf("unable to encode session payload: %w", err)
	}

	name := s.name
	if len(value)+len([]byte(name)) <= s.maxSize {
		http.SetCookie(w, &http.Cookie{
			Name:     s.name,
			Value:    string(value),
			Path:     s.cfg.Path,
			Domain:   s.cfg.Domain,
			MaxAge:   86400,
			HttpOnly: true,
			SameSite: parseSameSite(s.cfg.SameSite),
			Secure:   s.cfg.Secure,
		})
		return nil
	}

	// As we realistically won't exceed 99 cookies, subtract the size of two digits plus the '-' character.
	chunkSize := s.maxSize - (len(name) + 3)
	for i, val := range chunkBytes(value, chunkSize) {
		http.SetCookie(w, &http.Cookie{
			Name:     fmt.Sprintf("%s-%d", name, i+1),
			Value:    string(val),
			Path:     s.cfg.Path,
			Domain:   s.cfg.Domain,
			MaxAge:   86400,
			HttpOnly: true,
			SameSite: parseSameSite(s.cfg.SameSite),
			Secure:   s.cfg.Secure,
		})
	}
	return nil
}

// Update is the same as Create and only exists to satisfy the SessionStore interface.
func (s *CookieSessionStore) Update(w http.ResponseWriter, _ *http.Request, data SessionData) error {
	return s.Create(w, data)
}

// Delete sets the cookie on the HTTP response to be expired, effectively
// logging out its owner.
func (s *CookieSessionStore) Delete(w http.ResponseWriter, r *http.Request) error {
	cs := r.Cookies()

	for _, c := range cs {
		if strings.HasPrefix(c.Name, s.name) {
			http.SetCookie(w, &http.Cookie{
				Name:   c.Name,
				Path:   s.cfg.Path,
				Domain: s.cfg.Domain,
				MaxAge: -1, // Invalidates the cookie.
			})
		}
	}

	return nil
}

// Get retrieves the session from the request cookies.
func (s *CookieSessionStore) Get(r *http.Request) (*SessionData, error) {
	b := s.getCookiesBytes(r)
	if len(b) == 0 {
		return nil, nil
	}

	sess, err := s.decode(b)
	if err != nil {
		return nil, fmt.Errorf("unable to decode session: %w", err)
	}
	return &sess, nil
}

func (s *CookieSessionStore) getCookiesBytes(r *http.Request) []byte {
	if b, ok := getCookie(r, s.name); ok {
		return b
	}

	var data []byte
	for i := 1; i < maxCookies; i++ {
		b, ok := getCookie(r, fmt.Sprintf("%s-%d", s.name, i))
		if !ok {
			break
		}

		data = append(data, b...)
	}

	return data
}

// RemoveCookie removes the session cookie from the request.
func (s *CookieSessionStore) RemoveCookie(r *http.Request) {
	deleteCookie(r, s.name)
}

func (s *CookieSessionStore) encode(session SessionData) ([]byte, error) {
	blockSize := s.block.BlockSize()

	ser, err := json.Marshal(session)
	if err != nil {
		return nil, fmt.Errorf("unable to serialize session: %w", err)
	}

	encrypted := make([]byte, blockSize+len(ser))
	iv := s.rand.Bytes(blockSize)
	copy(encrypted[:blockSize], iv)
	stream := cipher.NewCTR(s.block, iv)
	stream.XORKeyStream(encrypted[blockSize:], ser)

	encoded := make([]byte, base64.RawURLEncoding.EncodedLen(len(encrypted)))
	base64.RawURLEncoding.Encode(encoded, encrypted)

	return encoded, nil
}

func (s *CookieSessionStore) decode(p []byte) (SessionData, error) {
	blockSize := s.block.BlockSize()

	decoded := make([]byte, base64.RawURLEncoding.DecodedLen(len(p)))
	if _, err := base64.RawURLEncoding.Decode(decoded, p); err != nil {
		return SessionData{}, fmt.Errorf("unable to decode session: %w", err)
	}

	decrypted := make([]byte, len(decoded)-blockSize)
	iv := decoded[:blockSize]
	stream := cipher.NewCTR(s.block, iv)
	stream.XORKeyStream(decrypted, decoded[blockSize:])

	var sess SessionData
	if err := json.Unmarshal(decrypted, &sess); err != nil {
		return SessionData{}, fmt.Errorf("unable to deserialize session: %w", err)
	}
	return sess, nil
}

func chunkBytes(b []byte, lim int) [][]byte {
	chunks := make([][]byte, 0, len(b)/lim+1)
	buf := bytes.NewBuffer(b)
	for {
		c := buf.Next(lim)
		if len(c) == 0 {
			break
		}

		chunks = append(chunks, c)
	}
	return chunks
}

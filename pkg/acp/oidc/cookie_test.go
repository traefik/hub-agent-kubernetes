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
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestCookieSessionStore_Delete(t *testing.T) {
	store, err := NewCookieSessionStore("test-name", &AuthSession{
		Secret:   "secret1234567890",
		Path:     "/",
		Domain:   "example.com",
		SameSite: "lax",
		Secure:   true,
	}, RandrMock{}, 200)
	require.NoError(t, err)

	req := httptest.NewRequest("GET", "http://foo.bar", nil)
	req.AddCookie(&http.Cookie{
		Name:  "test-name",
		Value: "value1",
	})
	req.AddCookie(&http.Cookie{
		Name:  "test-name-1",
		Value: "chunk cookies",
	})
	req.AddCookie(&http.Cookie{
		Name:  "test-name-2",
		Value: "chunk cookies part2",
	})
	req.AddCookie(&http.Cookie{
		Name:  "plop2-name",
		Value: "value2",
	})

	wantCookies := []string{
		"test-name=; Path=/; Domain=example.com; Max-Age=0",
		"test-name-1=; Path=/; Domain=example.com; Max-Age=0",
		"test-name-2=; Path=/; Domain=example.com; Max-Age=0",
	}
	w := httptest.NewRecorder()

	err = store.Delete(w, req)
	require.NoError(t, err)

	assert.Equal(t, wantCookies, w.Header().Values("Set-Cookie"))
}

func TestCookieSessionStore_RemoveCookieOnlyRemovesOurCookie(t *testing.T) {
	store, err := NewCookieSessionStore("test-name", &AuthSession{
		Secret:   "secret1234567890",
		Path:     "/",
		Domain:   "example.com",
		SameSite: "lax",
		Secure:   true,
	}, RandrMock{}, 200)
	require.NoError(t, err)

	req := httptest.NewRequest("GET", "http://foo.bar", nil)
	req.AddCookie(&http.Cookie{
		Name:  "test-name",
		Value: "value1",
	})
	req.AddCookie(&http.Cookie{
		Name:  "test-name-1",
		Value: "chunk cookies",
	})
	req.AddCookie(&http.Cookie{
		Name:  "test-name-2",
		Value: "chunk cookies part2",
	})
	req.AddCookie(&http.Cookie{
		Name:  "custom-name",
		Value: "value2",
	})

	store.RemoveCookie(req)

	assert.Equal(t, "custom-name=value2", req.Header.Get("Cookie"))
}

func TestCookieSessionStore_Create(t *testing.T) {
	store, err := NewCookieSessionStore("test-name", &AuthSession{
		Secret:   "secret1234567890",
		Path:     "/",
		Domain:   "example.com",
		SameSite: "lax",
		Secure:   true,
	}, RandrMock{}, 200)
	require.NoError(t, err)

	sess := SessionData{
		AccessToken: "test1",
		IDToken:     "test2",
	}

	rec := httptest.NewRecorder()

	err = store.Create(rec, sess)

	if !assert.NoError(t, err) {
		return
	}
	assert.Equal(t, "test-name=AQEBAQEBAQEBAQEBAQEBAQPCeonj6H8bgW-y-xdlkLmaN-_ouVkUUyQPAE3ccSugJPjEn0E6eB61jItErDH-XxhNXvoLnh92YAV1rATcOmBVdxP1Ahk4cwyUfBgI5_9x_42fkm4WB8NnvtMReWKFnYdOTBvPfLO1sh0; Path=/; Domain=example.com; Max-Age=86400; HttpOnly; Secure; SameSite=Lax", rec.Header().Get("Set-Cookie"))
}

func TestCookieSessionStore_CreateCanChunkCookies(t *testing.T) {
	store, err := NewCookieSessionStore("test-name", &AuthSession{
		Secret:   "secret1234567890",
		Path:     "/",
		Domain:   "example.com",
		SameSite: "lax",
		Secure:   true,
	}, RandrMock{}, 26)
	require.NoError(t, err)

	sess := SessionData{
		AccessToken: "test1",
		IDToken:     "test2",
	}

	rec := httptest.NewRecorder()

	err = store.Create(rec, sess)

	if !assert.NoError(t, err) {
		return
	}

	want := []string{
		"test-name-1=AQEBAQEBAQEBAQ; Path=/; Domain=example.com; Max-Age=86400; HttpOnly; Secure; SameSite=Lax",
		"test-name-2=EBAQEBAQPCeonj; Path=/; Domain=example.com; Max-Age=86400; HttpOnly; Secure; SameSite=Lax",
		"test-name-3=6H8bgW-y-xdlkL; Path=/; Domain=example.com; Max-Age=86400; HttpOnly; Secure; SameSite=Lax",
		"test-name-4=maN-_ouVkUUyQP; Path=/; Domain=example.com; Max-Age=86400; HttpOnly; Secure; SameSite=Lax",
		"test-name-5=AE3ccSugJPjEn0; Path=/; Domain=example.com; Max-Age=86400; HttpOnly; Secure; SameSite=Lax",
		"test-name-6=E6eB61jItErDH-; Path=/; Domain=example.com; Max-Age=86400; HttpOnly; Secure; SameSite=Lax",
		"test-name-7=XxhNXvoLnh92YA; Path=/; Domain=example.com; Max-Age=86400; HttpOnly; Secure; SameSite=Lax",
		"test-name-8=V1rATcOmBVdxP1; Path=/; Domain=example.com; Max-Age=86400; HttpOnly; Secure; SameSite=Lax",
		"test-name-9=Ahk4cwyUfBgI5_; Path=/; Domain=example.com; Max-Age=86400; HttpOnly; Secure; SameSite=Lax",
		"test-name-10=9x_42fkm4WB8Nn; Path=/; Domain=example.com; Max-Age=86400; HttpOnly; Secure; SameSite=Lax",
		"test-name-11=vtMReWKFnYdOTB; Path=/; Domain=example.com; Max-Age=86400; HttpOnly; Secure; SameSite=Lax",
		"test-name-12=vPfLO1sh0; Path=/; Domain=example.com; Max-Age=86400; HttpOnly; Secure; SameSite=Lax",
	}
	assert.Equal(t, want, rec.Header()["Set-Cookie"])
}

func TestCookieSessionStore_Update(t *testing.T) {
	store, err := NewCookieSessionStore("test-name", &AuthSession{
		Secret:   "secret1234567890",
		Path:     "/",
		Domain:   "example.com",
		SameSite: "lax",
		Secure:   true,
	}, RandrMock{}, 200)
	require.NoError(t, err)

	sess := SessionData{
		AccessToken: "test1",
		IDToken:     "test2",
	}

	rec := httptest.NewRecorder()

	err = store.Update(rec, nil, sess)

	if !assert.NoError(t, err) {
		return
	}
	assert.Equal(t, "test-name=AQEBAQEBAQEBAQEBAQEBAQPCeonj6H8bgW-y-xdlkLmaN-_ouVkUUyQPAE3ccSugJPjEn0E6eB61jItErDH-XxhNXvoLnh92YAV1rATcOmBVdxP1Ahk4cwyUfBgI5_9x_42fkm4WB8NnvtMReWKFnYdOTBvPfLO1sh0; Path=/; Domain=example.com; Max-Age=86400; HttpOnly; Secure; SameSite=Lax", rec.Header().Get("Set-Cookie"))
}

func TestCookieSessionStore_Get(t *testing.T) {
	store, err := NewCookieSessionStore("test-name", &AuthSession{
		Secret: "secret1234567890",
	}, RandrMock{}, 200)
	require.NoError(t, err)

	req := httptest.NewRequest("GET", "http://foo.bar", nil)
	req.AddCookie(&http.Cookie{
		Name:  "test-name",
		Value: "AQEBAQEBAQEBAQEBAQEBAQPCeonj6H8bgW-y-xdlkLmaN-_ouVkUUzkkP0fZQDzye_iK2BBiaG6t",
	})

	sess, err := store.Get(req)

	if !assert.NoError(t, err) {
		return
	}
	assert.Equal(t, "test1", sess.AccessToken)
	assert.Equal(t, "test2", sess.IDToken)
}

func TestCookieSessionStore_GetHandlesChunkedCookies(t *testing.T) {
	store, err := NewCookieSessionStore("test-name", &AuthSession{
		Secret: "secret1234567890",
	}, RandrMock{}, 200)
	require.NoError(t, err)

	req := httptest.NewRequest("GET", "http://foo.bar", nil)
	req.AddCookie(&http.Cookie{
		Name:  "test-name-1",
		Value: "AQEBAQEBAQEBAQEBAQEBAQPCeo",
	})
	req.AddCookie(&http.Cookie{
		Name:  "test-name-2",
		Value: "nj6H8bgW-y-xdlkLmaN-_ouVkU",
	})
	req.AddCookie(&http.Cookie{
		Name:  "test-name-3",
		Value: "UzkkP0fZQDzye_iK2BBiaG6t",
	})

	sess, err := store.Get(req)

	if !assert.NoError(t, err) {
		return
	}
	assert.Equal(t, "test1", sess.AccessToken)
	assert.Equal(t, "test2", sess.IDToken)
}

func TestCookieSessionStore_GetReturnsNilIfNoSessionExists(t *testing.T) {
	store, err := NewCookieSessionStore("test-name", &AuthSession{
		Secret: "secret1234567890",
	}, RandrMock{}, 200)
	require.NoError(t, err)

	req := httptest.NewRequest("GET", "http://foo.bar", nil)

	sess, err := store.Get(req)

	if !assert.NoError(t, err) {
		return
	}
	assert.Nil(t, sess)
}

type RandrMock struct{}

func (m RandrMock) Bytes(l int) []byte {
	b := make([]byte, l)
	for i := 0; i < l; i++ {
		b[i] = 1
	}
	return b
}

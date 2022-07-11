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

package platform

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestDomainCache_WarmUp(t *testing.T) {
	var callCount int

	mux := http.NewServeMux()
	mux.HandleFunc("/verified-domains", func(rw http.ResponseWriter, req *http.Request) {
		callCount++

		if req.Method != http.MethodGet {
			http.Error(rw, fmt.Sprintf("unsupported to method: %s", req.Method), http.StatusMethodNotAllowed)
			return
		}

		if req.Header.Get("Authorization") != "Bearer "+testToken {
			http.Error(rw, "Invalid token", http.StatusUnauthorized)
			return
		}

		payload := `["domain1.com", "domain2.io"]`
		_, err := rw.Write([]byte(payload))
		require.NoError(t, err)
	})

	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)

	client, err := NewClient(srv.URL, testToken)
	require.NoError(t, err)
	client.retryableHTTPClient = srv.Client()

	ttl := time.Millisecond
	domainCache := NewDomainCache(client, ttl)

	err = domainCache.WarmUp(context.Background())
	require.NoError(t, err)

	assert.Equal(t, 1, callCount)

	got := domainCache.ListVerifiedDomains(context.Background())
	assert.Equal(t, []string{"domain1.com", "domain2.io"}, got)
}

func TestDomainCache_WarmUp_unableToSetup(t *testing.T) {
	var callCount int

	mux := http.NewServeMux()
	mux.HandleFunc("/verified-domains", func(rw http.ResponseWriter, req *http.Request) {
		callCount++

		if req.Method != http.MethodGet {
			http.Error(rw, fmt.Sprintf("unsupported to method: %s", req.Method), http.StatusMethodNotAllowed)
			return
		}

		if req.Header.Get("Authorization") != "Bearer "+testToken {
			http.Error(rw, "Invalid token", http.StatusUnauthorized)
			return
		}

		rw.WriteHeader(http.StatusInternalServerError)
	})

	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)

	client, err := NewClient(srv.URL, testToken)
	require.NoError(t, err)
	client.retryableHTTPClient = srv.Client()

	ttl := time.Millisecond
	domainCache := NewDomainCache(client, ttl)

	err = domainCache.WarmUp(context.Background())
	require.Error(t, err)
	assert.Equal(t, 1, callCount)
}

func TestDomainCache_Run(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/verified-domains", func(rw http.ResponseWriter, req *http.Request) {
		if req.Method != http.MethodGet {
			http.Error(rw, fmt.Sprintf("unsupported to method: %s", req.Method), http.StatusMethodNotAllowed)
			return
		}

		if req.Header.Get("Authorization") != "Bearer "+testToken {
			http.Error(rw, "Invalid token", http.StatusUnauthorized)
			return
		}

		payload := `["domain1.com", "domain2.io"]`
		_, err := rw.Write([]byte(payload))
		require.NoError(t, err)
	})

	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)

	client, err := NewClient(srv.URL, testToken)
	require.NoError(t, err)
	client.retryableHTTPClient = srv.Client()

	ttl := 5 * time.Millisecond
	domainCache := NewDomainCache(client, ttl)
	ctx, cancelFunc := context.WithCancel(context.Background())
	dataAvailable := make(chan struct{})

	go func() {
		ticker := time.NewTicker(time.Millisecond)
		defer ticker.Stop()

		for range ticker.C {
			domainCache.verifiedMu.RLock()

			if len(domainCache.verified) != 0 {
				cancelFunc()
				close(dataAvailable)
				domainCache.verifiedMu.RUnlock()
				return
			}

			domainCache.verifiedMu.RUnlock()
		}
	}()

	go domainCache.Run(ctx)

	<-dataAvailable
	got := domainCache.ListVerifiedDomains(context.Background())
	assert.Equal(t, []string{"domain1.com", "domain2.io"}, got)
}

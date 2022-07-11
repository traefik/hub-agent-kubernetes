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
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"sync"
	"syscall"
	"testing"
	"time"

	"github.com/rs/zerolog"
	"github.com/rs/zerolog/log"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestMain(m *testing.M) {
	log.Logger = zerolog.New(io.Discard).With().Caller().Logger()
	code := m.Run()
	os.Exit(code)
}

func TestConfigWatcher_Run(t *testing.T) {
	cfg := Config{
		Metrics: MetricsConfig{Interval: 30 * time.Second},
	}

	client := setupClient(t, cfg)
	configWatcher := NewConfigWatcher(time.Millisecond, client)

	wait := make(chan struct{})
	var gotCfg Config
	listener := func(cfg Config) {
		gotCfg = cfg
		close(wait)
	}
	configWatcher.AddListener(listener)

	ctx := context.Background()
	go configWatcher.Run(ctx)

	select {
	case <-wait:
	case <-time.After(time.Second):
		t.Fatal("timed out")
	}

	assert.Equal(t, cfg, gotCfg)
}

func TestConfigWatcher_RunHandlesSIGHUP(t *testing.T) {
	cfg := Config{
		Metrics: MetricsConfig{Interval: 30 * time.Second},
	}
	client := setupClient(t, cfg)
	configWatcher := NewConfigWatcher(time.Hour, client)

	wait := make(chan struct{})
	var gotCfg Config
	var l sync.RWMutex
	var closed bool
	listener := func(cfg Config) {
		gotCfg = cfg

		l.Lock()
		if !closed {
			close(wait)
			closed = true
		}
		l.Unlock()
	}
	configWatcher.AddListener(listener)

	ctx := context.Background()
	go configWatcher.Run(ctx)

	go func() {
		for {
			l.RLock()
			if closed {
				l.RUnlock()
				return
			}
			l.RUnlock()

			pid := os.Getpid()
			err := syscall.Kill(pid, syscall.SIGHUP)
			require.NoError(t, err)
		}
	}()

	select {
	case <-wait:
	case <-time.After(time.Second):
		t.Fatal("timed out")
	}

	assert.Equal(t, cfg, gotCfg)
}

func setupClient(t *testing.T, cfg Config) *Client {
	t.Helper()

	mux := http.NewServeMux()
	mux.HandleFunc("/config", func(rw http.ResponseWriter, req *http.Request) {
		rw.Header().Set("Content-Type", "application/json")
		rw.WriteHeader(http.StatusOK)

		err := json.NewEncoder(rw).Encode(cfg)
		require.NoError(t, err)
	})

	srv := httptest.NewServer(mux)

	t.Cleanup(srv.Close)

	client, err := NewClient(srv.URL, "123")
	require.NoError(t, err)
	client.retryableHTTPClient = srv.Client()

	return client
}

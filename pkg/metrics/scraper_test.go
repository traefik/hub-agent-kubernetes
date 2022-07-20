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

package metrics_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strconv"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/traefik/hub-agent-kubernetes/pkg/metrics"
)

func TestScraper_ScrapeTraefik(t *testing.T) {
	srvURL := startServer(t, "testdata/traefik-metrics.txt")

	s := metrics.NewScraper(http.DefaultClient)

	got, err := s.Scrape(context.Background(), metrics.ParserTraefik, srvURL, metrics.ScrapeState{
		Ingresses: map[string]struct{}{"myIngress@default.ingress.networking.k8s.io": {}, "app-obe@whoami.ingress.networking.k8s.io": {}},
	})
	require.NoError(t, err)

	// router
	assert.Contains(t, got, &metrics.Histogram{Name: metrics.MetricRequestDuration, EdgeIngress: "myIngress@default", Sum: 0.0137623, Count: 1})
	assert.Contains(t, got, &metrics.Counter{Name: metrics.MetricRequests, EdgeIngress: "myIngress@default", Value: 2})
	// edge cases, TLS/middleware enable on entrypoint
	assert.Contains(t, got, &metrics.Counter{Name: metrics.MetricRequests, EdgeIngress: "app-obe@whoami", Value: 38})

	require.Len(t, got, 3)
}

func startServer(t *testing.T, file string) string {
	t.Helper()

	data, err := os.ReadFile(filepath.Clean(file))
	require.NoError(t, err)

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Length", strconv.Itoa(len(data)))
		w.WriteHeader(http.StatusOK)

		_, _ = w.Write(data)
	}))
	t.Cleanup(srv.Close)

	return srv.URL
}

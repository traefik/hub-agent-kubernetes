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
	"bytes"
	"compress/gzip"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strconv"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/traefik/hub-agent-kubernetes/pkg/acp"
	"github.com/traefik/hub-agent-kubernetes/pkg/acp/jwt"
	hubv1alpha1 "github.com/traefik/hub-agent-kubernetes/pkg/crd/api/hub/v1alpha1"
	"github.com/traefik/hub-agent-kubernetes/pkg/edgeingress"
	"github.com/traefik/hub-agent-kubernetes/pkg/topology/state"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

const testToken = "123"

func TestClient_Link(t *testing.T) {
	tests := []struct {
		desc             string
		returnStatusCode int
		wantClusterID    string
		wantErr          assert.ErrorAssertionFunc
	}{
		{
			desc:             "cluster successfully linked",
			returnStatusCode: http.StatusOK,
			wantClusterID:    "1",
			wantErr:          assert.NoError,
		},
		{
			desc:             "failed to link cluster",
			returnStatusCode: http.StatusTeapot,
			wantErr:          assert.Error,
			wantClusterID:    "",
		},
	}

	for _, test := range tests {
		test := test
		t.Run(test.desc, func(t *testing.T) {
			t.Parallel()

			var callCount int

			mux := http.NewServeMux()
			mux.HandleFunc("/link", func(rw http.ResponseWriter, req *http.Request) {
				callCount++

				if req.Method != http.MethodPost {
					http.Error(rw, fmt.Sprintf("unsupported to method: %s", req.Method), http.StatusMethodNotAllowed)
					return
				}

				if req.Header.Get("Authorization") != "Bearer "+testToken {
					http.Error(rw, "Invalid token", http.StatusUnauthorized)
					return
				}

				b, err := io.ReadAll(req.Body)
				if err != nil {
					http.Error(rw, err.Error(), http.StatusInternalServerError)
					return
				}

				if !bytes.Equal([]byte(`{"kubeId":"1","platform":"kubernetes","version":"dev"}`), b) {
					http.Error(rw, fmt.Sprintf("invalid body: %s", string(b)), http.StatusBadRequest)
					return
				}

				rw.WriteHeader(test.returnStatusCode)
				_, _ = rw.Write([]byte(`{"clusterId":"1"}`))
			})

			srv := httptest.NewServer(mux)

			t.Cleanup(srv.Close)

			c, err := NewClient(srv.URL, testToken)
			require.NoError(t, err)
			c.httpClient = srv.Client()

			hubClusterID, err := c.Link(context.Background(), "1")
			test.wantErr(t, err)

			require.Equal(t, 1, callCount)

			assert.Equal(t, test.wantClusterID, hubClusterID)
		})
	}
}

func TestClient_GetConfig(t *testing.T) {
	tests := []struct {
		desc             string
		returnStatusCode int
		wantConfig       Config
		wantErr          assert.ErrorAssertionFunc
	}{
		{
			desc:             "get config succeeds",
			returnStatusCode: http.StatusOK,
			wantConfig: Config{
				Metrics: MetricsConfig{
					Interval: time.Minute,
					Tables:   []string{"1m", "10m"},
				},
			},
			wantErr: assert.NoError,
		},
		{
			desc:             "get config fails",
			returnStatusCode: http.StatusTeapot,
			wantConfig:       Config{},
			wantErr:          assert.Error,
		},
	}

	for _, test := range tests {
		test := test
		t.Run(test.desc, func(t *testing.T) {
			t.Parallel()

			var callCount int

			mux := http.NewServeMux()
			mux.HandleFunc("/config", func(rw http.ResponseWriter, req *http.Request) {
				callCount++

				if req.Method != http.MethodGet {
					http.Error(rw, fmt.Sprintf("unsupported to method: %s", req.Method), http.StatusMethodNotAllowed)
					return
				}

				if req.Header.Get("Authorization") != "Bearer "+testToken {
					http.Error(rw, "Invalid token", http.StatusUnauthorized)
					return
				}

				rw.WriteHeader(test.returnStatusCode)
				_ = json.NewEncoder(rw).Encode(test.wantConfig)
			})

			srv := httptest.NewServer(mux)

			t.Cleanup(srv.Close)

			c, err := NewClient(srv.URL, testToken)
			require.NoError(t, err)
			c.httpClient = srv.Client()

			agentCfg, err := c.GetConfig(context.Background())
			test.wantErr(t, err)

			require.Equal(t, 1, callCount)

			assert.Equal(t, test.wantConfig, agentCfg)
		})
	}
}

func TestClient_Ping(t *testing.T) {
	tests := []struct {
		desc             string
		returnStatusCode int
		wantErr          assert.ErrorAssertionFunc
	}{
		{
			desc:             "ping successfully sent",
			returnStatusCode: http.StatusOK,
			wantErr:          assert.NoError,
		},
		{
			desc:             "ping sent for an unknown cluster",
			returnStatusCode: http.StatusNotFound,
			wantErr:          assert.Error,
		},
		{
			desc:             "error on ping",
			returnStatusCode: http.StatusInternalServerError,
			wantErr:          assert.Error,
		},
	}

	for _, test := range tests {
		test := test
		t.Run(test.desc, func(t *testing.T) {
			t.Parallel()

			var callCount int

			mux := http.NewServeMux()
			mux.HandleFunc("/ping", func(rw http.ResponseWriter, req *http.Request) {
				callCount++

				if req.Method != http.MethodPost {
					http.Error(rw, fmt.Sprintf("unsupported to method: %s", req.Method), http.StatusMethodNotAllowed)
					return
				}

				if req.Header.Get("Authorization") != "Bearer "+testToken {
					http.Error(rw, "Invalid token", http.StatusUnauthorized)
					return
				}

				rw.WriteHeader(test.returnStatusCode)
			})

			srv := httptest.NewServer(mux)

			t.Cleanup(srv.Close)

			c, err := NewClient(srv.URL, testToken)
			require.NoError(t, err)
			c.httpClient = srv.Client()

			err = c.Ping(context.Background())
			test.wantErr(t, err)

			require.Equal(t, 1, callCount)
		})
	}
}

func TestClient_ListVerifiedDomains(t *testing.T) {
	tests := []struct {
		desc             string
		returnStatusCode int
		domains          []string
		wantErr          assert.ErrorAssertionFunc
		wantDomains      []string
	}{
		{
			desc:             "get domains",
			returnStatusCode: http.StatusOK,
			domains:          []string{"domain.com"},
			wantErr:          assert.NoError,
			wantDomains:      []string{"domain.com"},
		},
		{
			desc:             "unable to get domains",
			returnStatusCode: http.StatusInternalServerError,
			wantErr:          assert.Error,
		},
	}

	for _, test := range tests {
		test := test
		t.Run(test.desc, func(t *testing.T) {
			t.Parallel()

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

				rw.WriteHeader(test.returnStatusCode)
				err := json.NewEncoder(rw).Encode(test.domains)
				require.NoError(t, err)
			})

			srv := httptest.NewServer(mux)

			t.Cleanup(srv.Close)

			c, err := NewClient(srv.URL, testToken)
			require.NoError(t, err)
			c.httpClient = srv.Client()

			domains, err := c.ListVerifiedDomains(context.Background())
			test.wantErr(t, err)

			require.Equal(t, 1, callCount)
			assert.Equal(t, test.wantDomains, domains)
		})
	}
}

func TestClient_CreateEdgeIngress(t *testing.T) {
	tests := []struct {
		desc             string
		createReq        *CreateEdgeIngressReq
		edgeIngress      *edgeingress.EdgeIngress
		returnStatusCode int
		wantErr          assert.ErrorAssertionFunc
	}{
		{
			desc: "create edge ingress",
			createReq: &CreateEdgeIngressReq{
				Name:      "name",
				Namespace: "namespace",
				Service: Service{
					Name: "service-name",
					Port: 8080,
				},
				ACP: &ACP{
					Name: "acp-name",
				},
			},
			returnStatusCode: http.StatusCreated,
			wantErr:          assert.NoError,
			edgeIngress: &edgeingress.EdgeIngress{
				WorkspaceID: "workspace-id",
				ClusterID:   "cluster-id",
				Namespace:   "namespace",
				Name:        "name",
				Domain:      "majestic-beaver-123.hub-traefik.io",
				Version:     "version-1",
				Service:     edgeingress.Service{Name: "service-name", Port: 8080},
				ACP:         &edgeingress.ACP{Name: "acp-name"},
				CreatedAt:   time.Now().UTC().Truncate(time.Millisecond),
				UpdatedAt:   time.Now().UTC().Truncate(time.Millisecond),
			},
		},
		{
			desc: "conflict",
			createReq: &CreateEdgeIngressReq{
				Name:      "name",
				Namespace: "namespace",
				Service: Service{
					Name: "service-name",
					Port: 8080,
				},
				ACP: &ACP{
					Name: "acp-name",
				},
			},
			returnStatusCode: http.StatusConflict,
			wantErr:          assertErrorIs(ErrVersionConflict),
		},
	}

	for _, test := range tests {
		test := test
		t.Run(test.desc, func(t *testing.T) {
			t.Parallel()

			var (
				callCount int
				callWith  hubv1alpha1.EdgeIngress
			)

			mux := http.NewServeMux()
			mux.HandleFunc("/edge-ingresses", func(rw http.ResponseWriter, req *http.Request) {
				callCount++

				if req.Method != http.MethodPost {
					http.Error(rw, fmt.Sprintf("unsupported to method: %s", req.Method), http.StatusMethodNotAllowed)
					return
				}

				if req.Header.Get("Authorization") != "Bearer "+testToken {
					http.Error(rw, "Invalid token", http.StatusUnauthorized)
					return
				}

				err := json.NewDecoder(req.Body).Decode(&callWith)
				require.NoError(t, err)

				rw.WriteHeader(test.returnStatusCode)
				err = json.NewEncoder(rw).Encode(test.edgeIngress)
				require.NoError(t, err)
			})

			srv := httptest.NewServer(mux)

			t.Cleanup(srv.Close)

			c, err := NewClient(srv.URL, testToken)
			require.NoError(t, err)
			c.httpClient = srv.Client()

			createdEdgeIngress, err := c.CreateEdgeIngress(context.Background(), test.createReq)
			test.wantErr(t, err)

			require.Equal(t, 1, callCount)
			assert.Equal(t, test.edgeIngress, createdEdgeIngress)
		})
	}
}

func TestClient_UpdateEdgeIngress(t *testing.T) {
	edgeIngress := hubv1alpha1.EdgeIngress{
		TypeMeta: metav1.TypeMeta{Kind: "EdgeIngress"},
		ObjectMeta: metav1.ObjectMeta{
			Name:      "name",
			Namespace: "namespace",
		},
		Spec: hubv1alpha1.EdgeIngressSpec{
			Service: hubv1alpha1.EdgeIngressService{
				Name: "service-name",
				Port: 8080,
			},
			ACP: &hubv1alpha1.EdgeIngressACP{
				Name: "acp-name",
			},
		},
	}
	edgeIngressWithStatus := edgeIngress
	edgeIngressWithStatus.Status.Version = "version-2"
	edgeIngressWithStatus.Status.Domain = "majestic-beaver-123.hub-traefik.io"

	tests := []struct {
		desc             string
		name             string
		namespace        string
		version          string
		updateReq        *UpdateEdgeIngressReq
		edgeIngress      *edgeingress.EdgeIngress
		returnStatusCode int
		wantErr          assert.ErrorAssertionFunc
	}{
		{
			desc:      "update edge ingress",
			name:      "name",
			namespace: "namespace",
			version:   "version-1",
			updateReq: &UpdateEdgeIngressReq{
				Service: Service{Name: "service-name", Port: 8080},
				ACP:     &ACP{Name: "acp-name"},
			},
			returnStatusCode: http.StatusOK,
			wantErr:          assert.NoError,
			edgeIngress: &edgeingress.EdgeIngress{
				WorkspaceID: "workspace-id",
				ClusterID:   "cluster-id",
				Namespace:   "namespace",
				Name:        "name",
				Domain:      "majestic-beaver-123.hub-traefik.io",
				Version:     "version-2",
				Service:     edgeingress.Service{Name: "service-name", Port: 8080},
				ACP:         &edgeingress.ACP{Name: "acp-name"},
				CreatedAt:   time.Now().Add(-time.Hour).UTC().Truncate(time.Millisecond),
				UpdatedAt:   time.Now().UTC().Truncate(time.Millisecond),
			},
		},
		{
			desc:    "conflict",
			version: "version-1",
			updateReq: &UpdateEdgeIngressReq{
				Service: Service{Name: "service-name", Port: 8080},
				ACP:     &ACP{Name: "acp-name"},
			},
			returnStatusCode: http.StatusConflict,
			wantErr:          assertErrorIs(ErrVersionConflict),
		},
	}

	for _, test := range tests {
		test := test
		t.Run(test.desc, func(t *testing.T) {
			t.Parallel()

			var (
				callCount int
				callWith  hubv1alpha1.EdgeIngress
			)

			id := test.name + "@" + test.namespace
			mux := http.NewServeMux()
			mux.HandleFunc("/edge-ingresses/"+id, func(rw http.ResponseWriter, req *http.Request) {
				callCount++

				if req.Method != http.MethodPut {
					http.Error(rw, fmt.Sprintf("unsupported to method: %s", req.Method), http.StatusMethodNotAllowed)
					return
				}

				if req.Header.Get("Authorization") != "Bearer "+testToken {
					http.Error(rw, "Invalid token", http.StatusUnauthorized)
					return
				}
				if req.Header.Get("Last-Known-Version") != test.version {
					http.Error(rw, "Invalid token", http.StatusInternalServerError)
					return
				}

				err := json.NewDecoder(req.Body).Decode(&callWith)
				require.NoError(t, err)

				rw.WriteHeader(test.returnStatusCode)
				err = json.NewEncoder(rw).Encode(test.edgeIngress)
				require.NoError(t, err)
			})

			srv := httptest.NewServer(mux)

			t.Cleanup(srv.Close)

			c, err := NewClient(srv.URL, testToken)
			require.NoError(t, err)
			c.httpClient = srv.Client()

			updatedEdgeIngress, err := c.UpdateEdgeIngress(context.Background(), test.namespace, test.name, test.version, test.updateReq)
			test.wantErr(t, err)

			require.Equal(t, 1, callCount)
			assert.Equal(t, test.edgeIngress, updatedEdgeIngress)
		})
	}
}

func TestClient_DeleteEdgeIngress(t *testing.T) {
	tests := []struct {
		desc             string
		version          string
		name             string
		namespace        string
		returnStatusCode int
		wantErr          assert.ErrorAssertionFunc
		edgeIngress      *hubv1alpha1.EdgeIngress
	}{
		{
			desc:             "delete edge ingress",
			version:          "version-1",
			name:             "name",
			namespace:        "namespace",
			returnStatusCode: http.StatusNoContent,
			wantErr:          assert.NoError,
		},
		{
			desc:             "conflict",
			version:          "version-1",
			name:             "name",
			namespace:        "namespace",
			returnStatusCode: http.StatusConflict,
			wantErr:          assertErrorIs(ErrVersionConflict),
		},
	}

	for _, test := range tests {
		test := test
		t.Run(test.desc, func(t *testing.T) {
			t.Parallel()

			var callCount int

			id := test.name + "@" + test.namespace
			mux := http.NewServeMux()
			mux.HandleFunc("/edge-ingresses/"+id, func(rw http.ResponseWriter, req *http.Request) {
				callCount++

				if req.Method != http.MethodDelete {
					http.Error(rw, fmt.Sprintf("unsupported to method: %s", req.Method), http.StatusMethodNotAllowed)
					return
				}

				if req.Header.Get("Authorization") != "Bearer "+testToken {
					http.Error(rw, "Invalid token", http.StatusUnauthorized)
					return
				}
				if req.Header.Get("Last-Known-Version") != test.version {
					http.Error(rw, "Invalid token", http.StatusInternalServerError)
					return
				}

				rw.WriteHeader(test.returnStatusCode)
			})

			srv := httptest.NewServer(mux)

			t.Cleanup(srv.Close)

			c, err := NewClient(srv.URL, testToken)
			require.NoError(t, err)
			c.httpClient = srv.Client()

			err = c.DeleteEdgeIngress(context.Background(), test.namespace, test.name, test.version)
			test.wantErr(t, err)

			require.Equal(t, 1, callCount)
		})
	}
}

func TestClient_CreateACP(t *testing.T) {
	tests := []struct {
		desc             string
		policy           *hubv1alpha1.AccessControlPolicy
		acp              *acp.ACP
		returnStatusCode int
		wantErr          assert.ErrorAssertionFunc
	}{
		{
			desc: "create access control policy",
			policy: &hubv1alpha1.AccessControlPolicy{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "name",
					Namespace: "namespace",
				},
				Spec: hubv1alpha1.AccessControlPolicySpec{
					JWT: &hubv1alpha1.AccessControlPolicyJWT{
						PublicKey: "key",
					},
				},
			},
			returnStatusCode: http.StatusCreated,
			wantErr:          assert.NoError,
			acp: &acp.ACP{
				Name:    "name",
				Version: "version-1",
				Config: acp.Config{
					JWT: &jwt.Config{
						PublicKey: "key",
					},
				},
			},
		},
		{
			desc: "conflict",
			policy: &hubv1alpha1.AccessControlPolicy{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "name",
					Namespace: "namespace",
				},
				Spec: hubv1alpha1.AccessControlPolicySpec{
					JWT: &hubv1alpha1.AccessControlPolicyJWT{
						PublicKey: "key",
					},
				},
			},
			returnStatusCode: http.StatusConflict,
			wantErr:          assertErrorIs(ErrVersionConflict),
		},
	}

	for _, test := range tests {
		test := test
		t.Run(test.desc, func(t *testing.T) {
			t.Parallel()

			var (
				callCount int
				callWith  acp.ACP
			)

			mux := http.NewServeMux()
			mux.HandleFunc("/acps", func(rw http.ResponseWriter, req *http.Request) {
				callCount++

				if req.Method != http.MethodPost {
					http.Error(rw, fmt.Sprintf("unsupported to method: %s", req.Method), http.StatusMethodNotAllowed)
					return
				}

				if req.Header.Get("Authorization") != "Bearer "+testToken {
					http.Error(rw, "Invalid token", http.StatusUnauthorized)
					return
				}

				err := json.NewDecoder(req.Body).Decode(&callWith)
				require.NoError(t, err)

				rw.WriteHeader(test.returnStatusCode)
				if test.returnStatusCode == http.StatusConflict {
					return
				}

				callWith.Version = "version-1"
				assert.Equal(t, test.acp, &callWith)

				err = json.NewEncoder(rw).Encode(callWith)
				require.NoError(t, err)
			})

			srv := httptest.NewServer(mux)

			t.Cleanup(srv.Close)

			c, err := NewClient(srv.URL, testToken)
			require.NoError(t, err)
			c.httpClient = srv.Client()

			createdACP, err := c.CreateACP(context.Background(), test.policy)
			test.wantErr(t, err)

			require.Equal(t, 1, callCount)
			assert.Equal(t, test.acp, createdACP)
		})
	}
}

func TestClient_UpdateACP(t *testing.T) {
	tests := []struct {
		desc             string
		policy           *hubv1alpha1.AccessControlPolicy
		acp              *acp.ACP
		returnStatusCode int
		wantErr          assert.ErrorAssertionFunc
	}{
		{
			desc: "update access control policy",
			policy: &hubv1alpha1.AccessControlPolicy{
				ObjectMeta: metav1.ObjectMeta{
					Name: "name",
				},
				Spec: hubv1alpha1.AccessControlPolicySpec{
					JWT: &hubv1alpha1.AccessControlPolicyJWT{
						PublicKey: "key",
					},
				},
			},
			returnStatusCode: http.StatusOK,
			wantErr:          assert.NoError,
			acp: &acp.ACP{
				Name:    "name",
				Version: "version-1",
				Config: acp.Config{
					JWT: &jwt.Config{
						PublicKey: "key",
					},
				},
			},
		},
		{
			desc: "conflict",
			policy: &hubv1alpha1.AccessControlPolicy{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "name",
					Namespace: "namespace",
				},
				Spec: hubv1alpha1.AccessControlPolicySpec{
					JWT: &hubv1alpha1.AccessControlPolicyJWT{
						PublicKey: "key",
					},
				},
			},
			returnStatusCode: http.StatusConflict,
			wantErr:          assertErrorIs(ErrVersionConflict),
		},
	}

	for _, test := range tests {
		test := test
		t.Run(test.desc, func(t *testing.T) {
			t.Parallel()

			var (
				callCount int
				callWith  acp.ACP
			)

			mux := http.NewServeMux()
			mux.HandleFunc("/acps/"+test.policy.Name, func(rw http.ResponseWriter, req *http.Request) {
				callCount++

				if req.Method != http.MethodPut {
					http.Error(rw, fmt.Sprintf("unsupported to method: %s", req.Method), http.StatusMethodNotAllowed)
					return
				}

				if req.Header.Get("Authorization") != "Bearer "+testToken {
					http.Error(rw, "Invalid token", http.StatusUnauthorized)
					return
				}

				if req.Header.Get("Last-Known-Version") != "oldVersion" {
					http.Error(rw, "Invalid token", http.StatusUnauthorized)
					return
				}

				err := json.NewDecoder(req.Body).Decode(&callWith)
				require.NoError(t, err)

				rw.WriteHeader(test.returnStatusCode)
				if test.returnStatusCode == http.StatusConflict {
					return
				}

				callWith.Version = "version-1"
				assert.Equal(t, test.acp, &callWith)

				err = json.NewEncoder(rw).Encode(callWith)
				require.NoError(t, err)
			})

			srv := httptest.NewServer(mux)

			t.Cleanup(srv.Close)

			c, err := NewClient(srv.URL, testToken)
			require.NoError(t, err)
			c.httpClient = srv.Client()

			updatedACP, err := c.UpdateACP(context.Background(), "oldVersion", test.policy)
			test.wantErr(t, err)

			require.Equal(t, 1, callCount)
			assert.Equal(t, test.acp, updatedACP)
		})
	}
}

func TestClient_DeleteACP(t *testing.T) {
	tests := []struct {
		desc             string
		name             string
		namespace        string
		returnStatusCode int
		wantErr          assert.ErrorAssertionFunc
	}{
		{
			desc:             "update access control policy",
			name:             "name",
			namespace:        "namespace",
			returnStatusCode: http.StatusNoContent,
			wantErr:          assert.NoError,
		},
		{
			desc:             "conflict",
			name:             "name",
			namespace:        "namespace",
			returnStatusCode: http.StatusConflict,
			wantErr:          assertErrorIs(ErrVersionConflict),
		},
	}

	for _, test := range tests {
		test := test
		t.Run(test.desc, func(t *testing.T) {
			t.Parallel()

			var callCount int
			mux := http.NewServeMux()
			mux.HandleFunc("/acps/"+test.name, func(rw http.ResponseWriter, req *http.Request) {
				callCount++

				if req.Method != http.MethodDelete {
					http.Error(rw, fmt.Sprintf("unsupported to method: %s", req.Method), http.StatusMethodNotAllowed)
					return
				}

				if req.Header.Get("Authorization") != "Bearer "+testToken {
					http.Error(rw, "Invalid token", http.StatusUnauthorized)
					return
				}
				if req.Header.Get("Last-Known-Version") != "oldVersion" {
					http.Error(rw, "Invalid token", http.StatusInternalServerError)
					return
				}

				rw.WriteHeader(test.returnStatusCode)
			})

			srv := httptest.NewServer(mux)

			t.Cleanup(srv.Close)

			c, err := NewClient(srv.URL, testToken)
			require.NoError(t, err)
			c.httpClient = srv.Client()

			err = c.DeleteACP(context.Background(), "oldVersion", test.name)
			test.wantErr(t, err)

			require.Equal(t, 1, callCount)
		})
	}
}

func TestClient_GetEdgeIngress(t *testing.T) {
	wantEdgeIngresses := []edgeingress.EdgeIngress{
		{
			WorkspaceID: "workspace-id",
			ClusterID:   "cluster-id",
			Namespace:   "namespace",
			Name:        "name",
			Domain:      "https://majestic-beaver-123.traefik-hub.io",
			Version:     "version",
			Service:     edgeingress.Service{Name: "service-name", Port: 8080},
			ACP:         &edgeingress.ACP{Name: "acp-name"},
			CreatedAt:   time.Now().Add(-time.Hour).UTC().Truncate(time.Millisecond),
			UpdatedAt:   time.Now().UTC().Truncate(time.Millisecond),
		},
	}

	var callCount int

	mux := http.NewServeMux()
	mux.HandleFunc("/edge-ingresses", func(rw http.ResponseWriter, req *http.Request) {
		callCount++

		if req.Method != http.MethodGet {
			http.Error(rw, fmt.Sprintf("unsupported to method: %s", req.Method), http.StatusMethodNotAllowed)
			return
		}

		if req.Header.Get("Authorization") != "Bearer "+testToken {
			http.Error(rw, "Invalid token", http.StatusUnauthorized)
			return
		}

		rw.WriteHeader(http.StatusOK)
		err := json.NewEncoder(rw).Encode(wantEdgeIngresses)
		require.NoError(t, err)
	})

	srv := httptest.NewServer(mux)

	t.Cleanup(srv.Close)

	c, err := NewClient(srv.URL, testToken)
	require.NoError(t, err)
	c.httpClient = srv.Client()

	gotEdgeIngresses, err := c.GetEdgeIngresses(context.Background())
	require.NoError(t, err)

	require.Equal(t, 1, callCount)
	assert.Equal(t, wantEdgeIngresses, gotEdgeIngresses)
}

func assertErrorIs(wantErr error) assert.ErrorAssertionFunc {
	return func(t assert.TestingT, err error, i ...interface{}) bool {
		return assert.ErrorIs(t, err, wantErr, i...)
	}
}

func TestClient_GetCertificate(t *testing.T) {
	tests := []struct {
		desc       string
		statusCode int
		wantCert   edgeingress.Certificate
		wantErr    error
	}{
		{
			desc:       "get certificate succeed",
			statusCode: http.StatusOK,
			wantCert: edgeingress.Certificate{
				Certificate: []byte("cert"),
				PrivateKey:  []byte("key"),
			},
		},
		{
			desc:       "get certificate unexpected error",
			statusCode: http.StatusTeapot,
			wantCert:   edgeingress.Certificate{},
			wantErr: &APIError{
				StatusCode: http.StatusTeapot,
				Message:    "error",
			},
		},
	}

	for _, test := range tests {
		test := test

		t.Run(test.desc, func(t *testing.T) {
			t.Parallel()

			var callCount int

			mux := http.NewServeMux()
			mux.HandleFunc("/wildcard-certificate", func(rw http.ResponseWriter, req *http.Request) {
				callCount++

				if req.Method != http.MethodGet {
					http.Error(rw, fmt.Sprintf("unsupported method: %s", req.Method), http.StatusMethodNotAllowed)
					return
				}

				if req.Header.Get("Authorization") != "Bearer "+testToken {
					http.Error(rw, "Invalid token", http.StatusUnauthorized)
					return
				}

				rw.WriteHeader(test.statusCode)

				switch test.statusCode {
				case http.StatusAccepted:
				case http.StatusOK:
					_ = json.NewEncoder(rw).Encode(test.wantCert)

				default:
					_ = json.NewEncoder(rw).Encode(APIError{Message: "error"})
				}
			})

			srv := httptest.NewServer(mux)
			t.Cleanup(srv.Close)

			c, err := NewClient(srv.URL, testToken)
			require.NoError(t, err)
			c.httpClient = srv.Client()

			gotCert, err := c.GetWildcardCertificate(context.Background())
			if test.wantErr != nil {
				require.ErrorAs(t, err, test.wantErr)
			} else {
				require.NoError(t, err)
			}

			assert.Equal(t, 1, callCount)
			assert.Equal(t, test.wantCert, gotCert)
		})
	}
}

func Test_GetCertificateByDomain(t *testing.T) {
	tests := []struct {
		desc       string
		statusCode int
		wantCert   edgeingress.Certificate
		wantErr    error
	}{
		{
			desc:       "get certificate succeed",
			statusCode: http.StatusOK,
			wantCert: edgeingress.Certificate{
				Certificate: []byte("cert"),
				PrivateKey:  []byte("key"),
			},
		},
		{
			desc:       "get certificate unexpected error",
			statusCode: http.StatusTeapot,
			wantCert:   edgeingress.Certificate{},
			wantErr: &APIError{
				StatusCode: http.StatusTeapot,
				Message:    "error",
			},
		},
	}

	for _, test := range tests {
		test := test

		t.Run(test.desc, func(t *testing.T) {
			t.Parallel()

			var callCount int

			mux := http.NewServeMux()
			mux.HandleFunc("/certificate", func(rw http.ResponseWriter, req *http.Request) {
				callCount++

				if req.Method != http.MethodGet {
					http.Error(rw, fmt.Sprintf("unsupported method: %s", req.Method), http.StatusMethodNotAllowed)
					return
				}

				if req.Header.Get("Authorization") != "Bearer 123" {
					http.Error(rw, "Invalid token", http.StatusUnauthorized)
					return
				}

				gotDomain := req.URL.Query()["domains"]
				assert.Equal(t, []string{"a.com", "b.com"}, gotDomain)

				rw.WriteHeader(test.statusCode)

				switch test.statusCode {
				case http.StatusAccepted:
				case http.StatusOK:
					_ = json.NewEncoder(rw).Encode(test.wantCert)

				default:
					_ = json.NewEncoder(rw).Encode(APIError{Message: "error"})
				}
			})

			srv := httptest.NewServer(mux)
			t.Cleanup(srv.Close)

			c, err := NewClient(srv.URL, "123")
			require.NoError(t, err)
			c.httpClient = srv.Client()

			gotCert, err := c.GetCertificateByDomains(context.Background(), []string{"a.com", "b.com"})
			if test.wantErr != nil {
				require.ErrorAs(t, err, test.wantErr)
			} else {
				require.NoError(t, err)
			}

			assert.Equal(t, 1, callCount)
			assert.Equal(t, test.wantCert, gotCert)
		})
	}
}

func TestClient_FetchTopology(t *testing.T) {
	tests := []struct {
		desc         string
		statusCode   int
		resp         []byte
		wantVersion  int64
		wantTopology state.Cluster
		wantErr      error
	}{
		{
			desc:       "fetch topology succeed",
			statusCode: http.StatusOK,
			resp: []byte(`{
				"version": 1,
				"topology": {
					"overview": {
						"serviceCount": 1
					},
					"services": {
						"service-1@ns": {
							"name": "service-1",
							"namespace": "ns",
							"type": "ClusterIP",
							"annotations": {"key": "value"},
							"externalIps": ["10.10.10.10"],
							"externalPorts": [8080, 8081]
						}
					}
				}
			}`),
			wantVersion: 1,
			wantTopology: state.Cluster{
				Services: map[string]*state.Service{
					"service-1@ns": {
						Name:          "service-1",
						Namespace:     "ns",
						Type:          "ClusterIP",
						Annotations:   map[string]string{"key": "value"},
						ExternalIPs:   []string{"10.10.10.10"},
						ExternalPorts: []int{8080, 8081},
					},
				},
			},
		},
		{
			desc:       "fetch topology unexpected error",
			statusCode: http.StatusTeapot,
			wantErr: &APIError{
				StatusCode: http.StatusTeapot,
				Message:    "error",
			},
		},
	}

	for _, test := range tests {
		test := test

		t.Run(test.desc, func(t *testing.T) {
			t.Parallel()

			var callCount int

			mux := http.NewServeMux()
			mux.HandleFunc("/topology", func(rw http.ResponseWriter, req *http.Request) {
				callCount++

				if req.Method != http.MethodGet {
					http.Error(rw, fmt.Sprintf("unsupported method: %s", req.Method), http.StatusMethodNotAllowed)
					return
				}

				if req.Header.Get("Authorization") != "Bearer 123" {
					http.Error(rw, "Invalid token", http.StatusUnauthorized)
					return
				}

				rw.WriteHeader(test.statusCode)

				switch test.statusCode {
				case http.StatusOK:
					_, _ = rw.Write(test.resp)
				default:
					_ = json.NewEncoder(rw).Encode(APIError{Message: "error"})
				}
			})

			srv := httptest.NewServer(mux)
			t.Cleanup(srv.Close)

			c, err := NewClient(srv.URL, "123")
			require.NoError(t, err)
			c.httpClient = srv.Client()

			gotTopology, gotVersion, err := c.FetchTopology(context.Background())
			if test.wantErr != nil {
				require.ErrorAs(t, err, test.wantErr)
			} else {
				require.NoError(t, err)
			}

			assert.Equal(t, 1, callCount)
			assert.Equal(t, test.wantVersion, gotVersion)
			assert.Equal(t, test.wantTopology, gotTopology)
		})
	}
}

func TestClient_PatchTopology(t *testing.T) {
	tests := []struct {
		desc             string
		statusCode       int
		patch            []byte
		lastKnownVersion int64
		resp             []byte
		wantVersion      int64
		wantErr          error
	}{
		{
			desc:       "patch topology succeed",
			statusCode: http.StatusOK,
			patch: []byte(`{
				"services": {
					"service-1@ns": null,
					"service-2@ns": {
						"externalPorts": [8080]
					}
				}
			}`),
			lastKnownVersion: 1,
			resp:             []byte(`{"version": 2}`),
			wantVersion:      2,
		},
		{
			desc:             "patch conflict",
			statusCode:       http.StatusConflict,
			patch:            []byte(`{"services": {"service-1@ns": null}}`),
			lastKnownVersion: 1,
			wantErr: &APIError{
				StatusCode: http.StatusConflict,
				Message:    "error",
			},
		},
		{
			desc:       "patch topology unexpected error",
			statusCode: http.StatusInternalServerError,
			wantErr: &APIError{
				StatusCode: http.StatusInternalServerError,
				Message:    "error",
			},
		},
	}

	for _, test := range tests {
		test := test

		t.Run(test.desc, func(t *testing.T) {
			t.Parallel()

			var callCount int

			mux := http.NewServeMux()
			mux.HandleFunc("/topology", func(rw http.ResponseWriter, req *http.Request) {
				callCount++

				if req.Method != http.MethodPatch {
					http.Error(rw, fmt.Sprintf("unsupported method: %s", req.Method), http.StatusMethodNotAllowed)
					return
				}

				if req.Header.Get("Authorization") != "Bearer 456" {
					http.Error(rw, "Invalid token", http.StatusUnauthorized)
					return
				}
				if req.Header.Get("Content-Type") != "application/merge-patch+json" {
					http.Error(rw, "Invalid Content-Type", http.StatusBadRequest)
					return
				}
				if req.Header.Get("Last-Known-Version") != strconv.FormatInt(test.lastKnownVersion, 10) {
					http.Error(rw, "Invalid Content-Type", http.StatusBadRequest)
					return
				}
				if req.Header.Get("Content-Encoding") != "gzip" {
					http.Error(rw, "Invalid Content-Encoding", http.StatusBadRequest)
					return
				}

				reader, err := gzip.NewReader(req.Body)
				if err != nil {
					http.Error(rw, err.Error(), http.StatusInternalServerError)
					return
				}
				defer func() { _ = reader.Close() }()

				body, err := io.ReadAll(reader)
				if err != nil {
					http.Error(rw, err.Error(), http.StatusInternalServerError)
					return
				}

				if !bytes.Equal(test.patch, body) {
					http.Error(rw, "invalid patch", http.StatusBadRequest)
					return
				}

				rw.WriteHeader(test.statusCode)

				switch test.statusCode {
				case http.StatusOK:
					_, _ = rw.Write(test.resp)
				default:
					_ = json.NewEncoder(rw).Encode(APIError{Message: "error"})
				}
			})

			srv := httptest.NewServer(mux)
			t.Cleanup(srv.Close)

			c, err := NewClient(srv.URL, "456")
			require.NoError(t, err)
			c.httpClient = srv.Client()

			gotVersion, err := c.PatchTopology(context.Background(), test.patch, test.lastKnownVersion)
			if test.wantErr != nil {
				require.ErrorAs(t, err, test.wantErr)
			} else {
				require.NoError(t, err)
			}

			assert.Equal(t, 1, callCount)
			assert.EqualValues(t, test.wantVersion, gotVersion)
		})
	}
}

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

package store

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/traefik/hub-agent-kubernetes/pkg/platform"
	"github.com/traefik/hub-agent-kubernetes/pkg/topology/state"
)

func TestStore_Write_fetchAndPatch(t *testing.T) {
	tests := []struct {
		desc            string
		fetchedVersion  string
		fetchedTopology state.Cluster
		newTopology     state.Cluster
		wantPatch       string
		wantVersion     string
	}{
		{
			desc:           "add one service",
			fetchedVersion: "version-1",
			fetchedTopology: state.Cluster{
				Overview: state.Overview{
					ServiceCount: 0,
				},
			},
			newTopology: state.Cluster{
				Overview: state.Overview{
					ServiceCount: 1,
				},
				Services: map[string]*state.Service{
					"service-1@ns": {
						Name:          "service-1",
						Namespace:     "ns",
						Type:          "ClusterIP",
						Annotations:   map[string]string{"key": "value"},
						ExternalIPs:   []string{"10.10.10.10"},
						ExternalPorts: []int{8080},
					},
				},
				Ingresses: map[string]*state.Ingress{
					"ingress-1@ns": {
						ResourceMeta: state.ResourceMeta{
							Name:      "ingress-1",
							Namespace: "ns",
						},
					},
				},
			},
			wantPatch: `{
				"overview": { "serviceCount":1 },
				"services": {
					"service-1@ns": {
						"annotations":{"key":"value"},
						"externalIPs":["10.10.10.10"],
						"externalPorts":[8080],
						"name":"service-1",
						"namespace":"ns",
						"type":"ClusterIP"
					}
				}
			}`,
			wantVersion: "version-2",
		},
		{
			desc:           "update a single service property",
			fetchedVersion: "version-1",
			fetchedTopology: state.Cluster{
				Overview: state.Overview{
					ServiceCount: 1,
				},
				Services: map[string]*state.Service{
					"service-1@ns": {
						Name:          "service-1",
						Namespace:     "ns",
						Type:          "ClusterIP",
						Annotations:   map[string]string{"key": "value"},
						ExternalIPs:   []string{"10.10.10.10"},
						ExternalPorts: []int{8080},
					},
				},
			},
			newTopology: state.Cluster{
				Overview: state.Overview{
					ServiceCount: 1,
				},
				Services: map[string]*state.Service{
					"service-1@ns": {
						Name:          "service-1",
						Namespace:     "ns",
						Type:          "ClusterIP",
						Annotations:   map[string]string{"key": "new-value"},
						ExternalIPs:   []string{"10.10.10.10"},
						ExternalPorts: []int{8080},
					},
				},
				Ingresses: map[string]*state.Ingress{
					"ingress-1@ns": {
						ResourceMeta: state.ResourceMeta{
							Name:      "ingress-1",
							Namespace: "ns",
						},
					},
				},
			},
			wantPatch: `{
				"services": {
					"service-1@ns": {
						"annotations":{"key":"new-value"}
					}
				}
			}`,
			wantVersion: "version-2",
		},
		{
			desc:           "delete a single service property",
			fetchedVersion: "version-1",
			fetchedTopology: state.Cluster{
				Overview: state.Overview{
					ServiceCount: 1,
				},
				Services: map[string]*state.Service{
					"service-1@ns": {
						Name:          "service-1",
						Namespace:     "ns",
						Type:          "ClusterIP",
						Annotations:   map[string]string{"key": "value"},
						ExternalIPs:   []string{"10.10.10.10"},
						ExternalPorts: []int{8080},
					},
				},
			},
			newTopology: state.Cluster{
				Overview: state.Overview{
					ServiceCount: 1,
				},
				Services: map[string]*state.Service{
					"service-1@ns": {
						Name:          "service-1",
						Namespace:     "ns",
						Type:          "ClusterIP",
						ExternalIPs:   []string{"10.10.10.10"},
						ExternalPorts: []int{8080},
					},
				},
				Ingresses: map[string]*state.Ingress{
					"ingress-1@ns": {
						ResourceMeta: state.ResourceMeta{
							Name:      "ingress-1",
							Namespace: "ns",
						},
					},
				},
			},
			wantPatch: `{
				"services": {
					"service-1@ns": {
						"annotations": null
					}
				}
			}`,
			wantVersion: "version-2",
		},
		{
			desc:           "added one port in a service",
			fetchedVersion: "version-1",
			fetchedTopology: state.Cluster{
				Overview: state.Overview{
					ServiceCount: 1,
				},
				Services: map[string]*state.Service{
					"service-1@ns": {
						Name:          "service-1",
						Namespace:     "ns",
						Type:          "ClusterIP",
						Annotations:   map[string]string{"key": "value"},
						ExternalIPs:   []string{"10.10.10.10"},
						ExternalPorts: []int{8080},
					},
				},
			},
			newTopology: state.Cluster{
				Overview: state.Overview{
					ServiceCount: 1,
				},
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
				Ingresses: map[string]*state.Ingress{
					"ingress-1@ns": {
						ResourceMeta: state.ResourceMeta{
							Name:      "ingress-1",
							Namespace: "ns",
						},
					},
				},
			},
			wantPatch: `{
				"services": {
					"service-1@ns": {
						"externalPorts": [8080, 8081]
					}
				}
			}`,
			wantVersion: "version-2",
		},
		{
			desc:           "delete a service",
			fetchedVersion: "version-1",
			fetchedTopology: state.Cluster{
				Overview: state.Overview{
					ServiceCount: 1,
				},
				Services: map[string]*state.Service{
					"service-1@ns": {
						Name:          "service-1",
						Namespace:     "ns",
						Type:          "ClusterIP",
						Annotations:   map[string]string{"key": "value"},
						ExternalIPs:   []string{"10.10.10.10"},
						ExternalPorts: []int{8080},
					},
				},
			},
			newTopology: state.Cluster{
				Overview: state.Overview{
					ServiceCount: 0,
				},
				Services: map[string]*state.Service{},
				Ingresses: map[string]*state.Ingress{
					"ingress-1@ns": {
						ResourceMeta: state.ResourceMeta{
							Name:      "ingress-1",
							Namespace: "ns",
						},
					},
				},
			},
			wantPatch: `{
				"overview": {
					"serviceCount": 0
				},
				"services": {
					"service-1@ns": null
				}
			}`,
			wantVersion: "version-2",
		},
		{
			desc:           "mixed update and delete",
			fetchedVersion: "version-1",
			fetchedTopology: state.Cluster{
				Overview: state.Overview{
					ServiceCount: 1,
				},
				Services: map[string]*state.Service{
					"service-1@ns": {
						Name:          "service-1",
						Namespace:     "ns",
						Type:          "ClusterIP",
						Annotations:   map[string]string{"key": "value"},
						ExternalIPs:   []string{"10.10.10.10"},
						ExternalPorts: []int{8080},
					},
					"service-2@ns": {
						Name:          "service-2",
						Namespace:     "ns",
						Type:          "ClusterIP",
						Annotations:   map[string]string{"key": "value"},
						ExternalIPs:   []string{"10.10.10.11"},
						ExternalPorts: []int{8082},
					},
				},
			},
			newTopology: state.Cluster{
				Overview: state.Overview{
					ServiceCount: 1,
				},
				Services: map[string]*state.Service{
					"service-2@ns": {
						Name:        "service-2",
						Namespace:   "ns",
						Type:        "ClusterIP",
						Annotations: map[string]string{"key": "new-value"},
						ExternalIPs: []string{"10.10.10.12"},
					},
				},
				Ingresses: map[string]*state.Ingress{
					"ingress-1@ns": {
						ResourceMeta: state.ResourceMeta{
							Name:      "ingress-1",
							Namespace: "ns",
						},
					},
				},
			},
			wantPatch: `{
				"services": {
					"service-1@ns": null,
					"service-2@ns": {
						"annotations":{"key":"new-value"},
						"externalIPs": ["10.10.10.12"],
						"externalPorts": null
					}
				}
			}`,
			wantVersion: "version-2",
		},
		{
			desc:           "no different",
			fetchedVersion: "version-1",
			fetchedTopology: state.Cluster{
				Overview: state.Overview{
					ServiceCount: 1,
				},
				Services: map[string]*state.Service{
					"service-1@ns": {
						Name:          "service-1",
						Namespace:     "ns",
						Type:          "ClusterIP",
						Annotations:   map[string]string{"key": "value"},
						ExternalIPs:   []string{"10.10.10.10"},
						ExternalPorts: []int{8080},
					},
				},
			},
			newTopology: state.Cluster{
				Overview: state.Overview{
					ServiceCount: 1,
				},
				Services: map[string]*state.Service{
					"service-1@ns": {
						Name:          "service-1",
						Namespace:     "ns",
						Type:          "ClusterIP",
						Annotations:   map[string]string{"key": "value"},
						ExternalIPs:   []string{"10.10.10.10"},
						ExternalPorts: []int{8080},
					},
				},
			},
			wantVersion: "version-1",
		},
	}

	for _, test := range tests {
		test := test

		t.Run(test.desc, func(t *testing.T) {
			t.Parallel()

			platformClient := newPlatformClientMock(t).
				OnFetchTopology().TypedReturns(test.fetchedTopology, test.fetchedVersion, nil).Once()

			if test.wantPatch != "" {
				patch := []byte(removeSpaces(test.wantPatch))

				platformClient.OnPatchTopology(patch, test.fetchedVersion).TypedReturns(test.wantVersion, nil).Once()
			}

			s := New(platformClient.Parent)

			err := s.Write(context.Background(), test.newTopology)
			require.NoError(t, err)

			assert.Equal(t, test.wantVersion, s.lastKnownVersion)
			assert.NotEmpty(t, s.lastTopology)
		})
	}
}

func TestStore_Write_alreadyFetched(t *testing.T) {
	platformClient := newPlatformClientMock(t).
		OnPatchTopology([]byte(removeSpaces(`{
			"services": {
				"service-1@ns": {
					"annotations":{"key":"new-value"}
				}
			}
		}`)), "version-1").
		TypedReturns("version-2", nil).
		Once().
		Parent

	var err error

	s := New(platformClient)
	s.lastKnownVersion = "version-1"
	s.lastTopology, err = json.Marshal(state.Cluster{
		Overview: state.Overview{
			ServiceCount: 1,
		},
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
	})
	require.NoError(t, err)

	newTopology := state.Cluster{
		Overview: state.Overview{
			ServiceCount: 1,
		},
		Services: map[string]*state.Service{
			"service-1@ns": {
				Name:          "service-1",
				Namespace:     "ns",
				Type:          "ClusterIP",
				Annotations:   map[string]string{"key": "new-value"},
				ExternalIPs:   []string{"10.10.10.10"},
				ExternalPorts: []int{8080, 8081},
			},
		},
	}

	err = s.Write(context.Background(), newTopology)
	require.NoError(t, err)

	assert.Equal(t, "version-2", s.lastKnownVersion)
}

func TestStore_Write_retryOnPatchRetryableFailure(t *testing.T) {
	platformClient := newPlatformClientMock(t).
		OnFetchTopology().
		TypedReturns(state.Cluster{
			Overview: state.Overview{ServiceCount: 1},
			Services: map[string]*state.Service{
				"service-1@ns": {Name: "service-1", Namespace: "ns", ExternalPorts: []int{8080}},
			},
		}, "version-1", nil).Once().
		OnFetchTopology().
		TypedReturns(state.Cluster{
			Overview: state.Overview{ServiceCount: 1},
			Services: map[string]*state.Service{
				"service-1@ns": {Name: "service-1", Namespace: "ns", ExternalPorts: []int{8080, 8081}},
			},
		}, "version-2", nil).Once().
		OnFetchTopology().
		TypedReturns(state.Cluster{
			Overview: state.Overview{ServiceCount: 1},
			Services: map[string]*state.Service{
				"service-1@ns": {Name: "service-1", Namespace: "ns", ExternalPorts: []int{8080, 8081, 8082}},
			},
		}, "version-3", nil).Once().
		OnPatchTopology([]byte(removeSpaces(`{
			"services": {
				"service-1@ns": {
					"annotations": {"key":"value"}
				}
			}
		}`)), "version-1").TypedReturns("", platform.APIError{Retryable: true}).Once().
		OnPatchTopology([]byte(removeSpaces(`{
			"services": {
				"service-1@ns": {
					"annotations": {"key":"value"},
					"externalPorts": [8080]
				}
			}
		}`)), "version-2").TypedReturns("", platform.APIError{Retryable: true}).Once().
		OnPatchTopology([]byte(removeSpaces(`{
			"services": {
				"service-1@ns": {
					"annotations": {"key":"value"},
					"externalPorts": [8080]
				}
			}
		}`)), "version-3").TypedReturns("version-4", nil).Once().
		Parent

	s := New(platformClient)

	newTopology := state.Cluster{
		Overview: state.Overview{
			ServiceCount: 1,
		},
		Services: map[string]*state.Service{
			"service-1@ns": {
				Name:          "service-1",
				Namespace:     "ns",
				Annotations:   map[string]string{"key": "value"},
				ExternalPorts: []int{8080},
			},
		},
	}

	err := s.Write(context.Background(), newTopology)
	require.NoError(t, err)
	assert.Equal(t, "version-4", s.lastKnownVersion)

	// Apply the same topology and make sure it did nothing.
	err = s.Write(context.Background(), newTopology)
	require.NoError(t, err)
	assert.Equal(t, "version-4", s.lastKnownVersion)
}

func TestStore_Write_doNotRetryOnPatchFatalFailure(t *testing.T) {
	platformClient := newPlatformClientMock(t).
		OnFetchTopology().TypedReturns(state.Cluster{Overview: state.Overview{ServiceCount: 1}}, "version-1", nil).Once().
		OnPatchTopology([]byte(`{"overview":{"serviceCount":42}}`), "version-1").TypedReturns("", errors.New("boom")).Once().
		OnFetchTopology().TypedReturns(state.Cluster{Overview: state.Overview{ServiceCount: 1}}, "version-2", nil).Once().
		OnPatchTopology([]byte(`{"overview":{"serviceCount":42}}`), "version-2").TypedReturns("", platform.APIError{Retryable: false}).Once().
		OnFetchTopology().TypedReturns(state.Cluster{Overview: state.Overview{ServiceCount: 1}}, "version-3", nil).Once().
		OnPatchTopology([]byte(`{"overview":{"serviceCount":42}}`), "version-3").TypedReturns("version-4", nil).Once().
		Parent

	s := New(platformClient)

	newTopology := state.Cluster{
		Overview: state.Overview{
			ServiceCount: 42,
		},
	}

	err := s.Write(context.Background(), newTopology)
	require.Error(t, err)
	assert.Equal(t, "", s.lastKnownVersion)

	err = s.Write(context.Background(), newTopology)
	require.Error(t, err)
	assert.Equal(t, "", s.lastKnownVersion)

	// Apply the same topology with a successful patch.
	err = s.Write(context.Background(), newTopology)
	require.NoError(t, err)
	assert.Equal(t, "version-4", s.lastKnownVersion)
}

func TestStore_Write_abortOnFetchFailure(t *testing.T) {
	platformClient := newPlatformClientMock(t).
		OnFetchTopology().TypedReturns(state.Cluster{}, "", errors.New("boom")).Once().
		OnFetchTopology().
		TypedReturns(state.Cluster{
			Overview: state.Overview{ServiceCount: 1},
			Services: map[string]*state.Service{
				"service-1@ns": {Name: "service-1", Namespace: "ns", ExternalPorts: []int{8080}},
			},
		}, "version-1", nil).Once().
		OnPatchTopology([]byte(removeSpaces(`{
			"services": {
				"service-1@ns": {
					"annotations": {"key":"value"}
				}
			}
		}`)), "version-1").TypedReturns("version-2", nil).Once().
		Parent

	s := New(platformClient)

	newTopology := state.Cluster{
		Overview: state.Overview{
			ServiceCount: 1,
		},
		Services: map[string]*state.Service{
			"service-1@ns": {
				Name:          "service-1",
				Namespace:     "ns",
				Annotations:   map[string]string{"key": "value"},
				ExternalPorts: []int{8080},
			},
		},
	}

	err := s.Write(context.Background(), newTopology)
	require.Error(t, err)
	assert.Equal(t, "", s.lastKnownVersion)

	// Make sure that if the fetch didn't fail the next time it will patch the topology successfully.
	err = s.Write(context.Background(), newTopology)
	require.NoError(t, err)
	assert.Equal(t, "version-2", s.lastKnownVersion)
}

func TestStore_Write_giveUpOnRetryingIfMaxRetryReached(t *testing.T) {
	platformClient := newPlatformClientMock(t).
		OnFetchTopology().TypedReturns(state.Cluster{Overview: state.Overview{ServiceCount: 1}}, "version-1", nil).Once().
		OnFetchTopology().TypedReturns(state.Cluster{Overview: state.Overview{ServiceCount: 2}}, "version-2", nil).Once().
		OnFetchTopology().TypedReturns(state.Cluster{Overview: state.Overview{ServiceCount: 3}}, "version-3", nil).Once().
		OnPatchTopology([]byte(`{"overview":{"serviceCount":42}}`), "version-1").TypedReturns("", platform.APIError{Retryable: true}).Once().
		OnPatchTopology([]byte(`{"overview":{"serviceCount":42}}`), "version-2").TypedReturns("", platform.APIError{Retryable: true}).Once().
		OnPatchTopology([]byte(`{"overview":{"serviceCount":42}}`), "version-3").TypedReturns("", platform.APIError{Retryable: true}).Once().
		OnFetchTopology().TypedReturns(state.Cluster{Overview: state.Overview{ServiceCount: 1}}, "version-4", nil).Once().
		OnPatchTopology([]byte(`{"overview":{"serviceCount":42}}`), "version-4").TypedReturns("version-5", nil).Once().
		Parent

	s := New(platformClient)
	s.maxPatchRetry = 3

	newTopology := state.Cluster{
		Overview: state.Overview{
			ServiceCount: 42,
		},
	}

	err := s.Write(context.Background(), newTopology)
	require.Error(t, err)
	assert.Equal(t, "", s.lastKnownVersion)

	// Apply the same topology with a successful patch.
	err = s.Write(context.Background(), newTopology)
	require.NoError(t, err)
	assert.Equal(t, "version-5", s.lastKnownVersion)
}

func removeSpaces(s string) string {
	s = strings.ReplaceAll(s, "\n", "")
	s = strings.ReplaceAll(s, " ", "")
	s = strings.ReplaceAll(s, "\t", "")

	return s
}

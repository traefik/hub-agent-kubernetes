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
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/traefik/hub-agent-kubernetes/pkg/topology/state"
	netv1 "k8s.io/api/networking/v1"
)

const (
	commitCommand = "commit"
	pushCommand   = "push"
)

func TestWrite_GitNoChanges(t *testing.T) {
	tmpDir := t.TempDir()

	var (
		pushCallCount   int
		commitCallCount int
	)
	s := &Store{
		workingDir: tmpDir,
		gitExecutor: func(_ context.Context, _ string, _ bool, args ...string) (string, error) {
			switch args[0] {
			case pushCommand:
				pushCallCount++
			case commitCommand:
				commitCallCount++
				return "nothing to commit", errors.New("fake error")
			}

			return "", nil
		},
	}

	err := s.Write(context.Background(), &state.Cluster{ID: "myclusterID"})
	require.NoError(t, err)

	assert.Equal(t, 1, commitCallCount)
	assert.Equal(t, 0, pushCallCount)
}

func TestWrite_GitChanges(t *testing.T) {
	tmpDir := t.TempDir()

	var pushCallCount int
	s := &Store{
		workingDir: tmpDir,
		gitExecutor: func(_ context.Context, _ string, _ bool, args ...string) (string, error) {
			if args[0] == pushCommand {
				pushCallCount++
			}
			return "", nil
		},
	}

	err := s.Write(context.Background(), &state.Cluster{ID: "myclusterID"})
	require.NoError(t, err)

	assert.Equal(t, 1, pushCallCount)
}

func TestWrite_Ingresses(t *testing.T) {
	tmpDir := t.TempDir()

	testIngress := &state.Ingress{
		ResourceMeta: state.ResourceMeta{
			Kind:      "kind",
			Group:     "group",
			Name:      "name",
			Namespace: "namespace",
		},
		IngressMeta: state.IngressMeta{
			ClusterID:      "cluster-id",
			ControllerType: "controller",
			Annotations: map[string]string{
				"foo": "bar",
			},
		},
		TLS: []netv1.IngressTLS{
			{
				Hosts:      []string{"foo.com"},
				SecretName: "secret",
			},
		},
		Rules: []netv1.IngressRule{
			{
				Host: "foo.com",
				IngressRuleValue: netv1.IngressRuleValue{
					HTTP: &netv1.HTTPIngressRuleValue{
						Paths: []netv1.HTTPIngressPath{
							{
								Path:     "/",
								PathType: pathTypePtr(netv1.PathTypeExact),
								Backend: netv1.IngressBackend{
									Service: &netv1.IngressServiceBackend{
										Name: "service",
										Port: netv1.ServiceBackendPort{
											Number: 80,
										},
									},
								},
							},
						},
					},
				},
			},
		},
		Services: []string{"service@namespace"},
	}

	var pushCallCount int
	s := &Store{
		workingDir: tmpDir,
		gitExecutor: func(_ context.Context, _ string, _ bool, args ...string) (string, error) {
			if args[0] == pushCommand {
				pushCallCount++
			}
			return "", nil
		},
	}

	err := s.Write(context.Background(), &state.Cluster{
		Ingresses: map[string]*state.Ingress{
			"name@namespace.kind.group": testIngress,
		},
	})
	require.NoError(t, err)

	assert.Equal(t, 1, pushCallCount)

	got := readTopology(t, tmpDir)

	var gotIng state.Ingress
	err = json.Unmarshal(got["/Ingresses/name@namespace.kind.group.json"], &gotIng)
	require.NoError(t, err)

	assert.Equal(t, testIngress, &gotIng)
}

func readTopology(t *testing.T, dir string) map[string][]byte {
	t.Helper()

	result := make(map[string][]byte)
	err := filepath.Walk(dir, func(path string, info os.FileInfo, err error) error {
		if !info.IsDir() {
			if path == "./" {
				return nil
			}

			data, err := os.ReadFile(path)
			require.NoError(t, err)

			result[strings.TrimPrefix(path, dir)] = data
		}
		return nil
	})
	require.NoError(t, err)

	return result
}

func pathTypePtr(pathType netv1.PathType) *netv1.PathType {
	return &pathType
}

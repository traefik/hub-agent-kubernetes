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

package state

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	netv1 "k8s.io/api/networking/v1"
	netv1beta1 "k8s.io/api/networking/v1beta1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	kubemock "k8s.io/client-go/kubernetes/fake"
)

func Test_watchAll_handlesUnsupportedVersions(t *testing.T) {
	tests := []struct {
		desc          string
		serverVersion string
		wantErr       assert.ErrorAssertionFunc
	}{
		{
			desc:    "Empty",
			wantErr: assert.Error,
		},
		{
			desc:          "Malformed version",
			serverVersion: "foobar",
			wantErr:       assert.Error,
		},
		{
			desc:          "Unsupported version",
			serverVersion: "v1.13",
			wantErr:       assert.Error,
		},
		{
			desc:          "Supported version",
			serverVersion: "v1.16",
			wantErr:       assert.NoError,
		},
	}

	for _, test := range tests {
		test := test
		t.Run(test.desc, func(t *testing.T) {
			t.Parallel()

			kubeClient := kubemock.NewSimpleClientset()

			_, err := watchAll(context.Background(), kubeClient, test.serverVersion)

			test.wantErr(t, err)
		})
	}
}

func Test_watchAll_handlesAllIngressAPIVersions(t *testing.T) {
	tests := []struct {
		desc          string
		serverVersion string
		want          map[string]*Ingress
	}{
		{
			desc:          "v1.16",
			serverVersion: "v1.16",
			want: map[string]*Ingress{
				"myIngress_netv1beta1@myns.ingress.networking.k8s.io": {
					ResourceMeta: ResourceMeta{
						Kind:      "Ingress",
						Group:     "networking.k8s.io",
						Name:      "myIngress_netv1beta1",
						Namespace: "myns",
					},
				},
			},
		},
		{
			desc:          "v1.18",
			serverVersion: "v1.18",
			want: map[string]*Ingress{
				"myIngress_netv1beta1@myns.ingress.networking.k8s.io": {
					ResourceMeta: ResourceMeta{
						Kind:      "Ingress",
						Group:     "networking.k8s.io",
						Name:      "myIngress_netv1beta1",
						Namespace: "myns",
					},
				},
			},
		},
		{
			desc:          "v1.18.10",
			serverVersion: "v1.18.10",
			want: map[string]*Ingress{
				"myIngress_netv1beta1@myns.ingress.networking.k8s.io": {
					ResourceMeta: ResourceMeta{
						Kind:      "Ingress",
						Group:     "networking.k8s.io",
						Name:      "myIngress_netv1beta1",
						Namespace: "myns",
					},
				},
			},
		},
		{
			desc:          "v1.19",
			serverVersion: "v1.19",
			want: map[string]*Ingress{
				"myIngress_netv1@myns.ingress.networking.k8s.io": {
					ResourceMeta: ResourceMeta{
						Kind:      "Ingress",
						Group:     "networking.k8s.io",
						Name:      "myIngress_netv1",
						Namespace: "myns",
					},
				},
			},
		},
		{
			desc:          "v1.22",
			serverVersion: "v1.22",
			want: map[string]*Ingress{
				"myIngress_netv1@myns.ingress.networking.k8s.io": {
					ResourceMeta: ResourceMeta{
						Kind:      "Ingress",
						Group:     "networking.k8s.io",
						Name:      "myIngress_netv1",
						Namespace: "myns",
					},
				},
			},
		},
	}

	for _, test := range tests {
		test := test
		t.Run(test.desc, func(t *testing.T) {
			t.Parallel()

			k8sObjects := []runtime.Object{
				&netv1beta1.Ingress{
					ObjectMeta: metav1.ObjectMeta{
						Namespace: "myns",
						Name:      "myIngress_netv1beta1",
					},
				},
				&netv1.Ingress{
					ObjectMeta: metav1.ObjectMeta{
						Namespace: "myns",
						Name:      "myIngress_netv1",
					},
				},
			}

			kubeClient := kubemock.NewSimpleClientset(k8sObjects...)

			f, err := watchAll(context.Background(), kubeClient, test.serverVersion)
			require.NoError(t, err)

			got, err := f.getIngresses()
			require.NoError(t, err)

			assert.Equal(t, test.want, got)
		})
	}
}

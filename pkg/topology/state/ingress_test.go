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
	"os"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	corev1 "k8s.io/api/core/v1"
	netv1 "k8s.io/api/networking/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	kubemock "k8s.io/client-go/kubernetes/fake"
	"k8s.io/client-go/kubernetes/scheme"
)

func TestFetcher_GetIngresses(t *testing.T) {
	want := map[string]*Ingress{
		"myIngress@myns.ingress.networking.k8s.io": {
			ResourceMeta: ResourceMeta{
				Kind:      "Ingress",
				Group:     "networking.k8s.io",
				Name:      "myIngress",
				Namespace: "myns",
			},
			IngressMeta: IngressMeta{
				Annotations: map[string]string{
					"cert-manager.io/cluster-issuer": "foo",
				},
				ControllerType: "myIngressController",
			},
			IngressClassName: stringPtr("myIngressClass"),
			TLS: []netv1.IngressTLS{
				{
					Hosts:      []string{"foo.com"},
					SecretName: "mySecret",
				},
			},
			Rules: []netv1.IngressRule{
				{
					Host: "foo.bar",
					IngressRuleValue: netv1.IngressRuleValue{
						HTTP: &netv1.HTTPIngressRuleValue{
							Paths: []netv1.HTTPIngressPath{
								{
									Backend: netv1.IngressBackend{
										Service: &netv1.IngressServiceBackend{
											Name: "myService",
										},
									},
								},
							},
						},
					},
				},
			},
			DefaultBackend: &netv1.IngressBackend{
				Service: &netv1.IngressServiceBackend{
					Name: "myDefaultService",
				},
			},
			Services: []string{"myDefaultService@myns", "myService@myns"},
		},
	}

	objects := loadK8sObjects(t, "fixtures/ingress/one-ingress-matches-ingress-class.yml")

	kubeClient := kubemock.NewSimpleClientset(objects...)

	f, err := watchAll(context.Background(), kubeClient, "v1.20.1")
	require.NoError(t, err)

	got, err := f.getIngresses()
	require.NoError(t, err)

	assert.Equal(t, want, got)
}

func TestFetcher_FetchIngresses(t *testing.T) {
	want := []*netv1.Ingress{
		{
			ObjectMeta: metav1.ObjectMeta{
				Namespace: "myns",
				Name:      "myIngress_v1beta1",
			},
			Spec: netv1.IngressSpec{
				IngressClassName: stringPtr("myIngressClass"),
				DefaultBackend: &netv1.IngressBackend{
					Service: &netv1.IngressServiceBackend{
						Name: "myService",
						Port: netv1.ServiceBackendPort{
							Number: 443,
						},
					},
				},
				Rules: []netv1.IngressRule{
					{
						Host: "foo.bar",
						IngressRuleValue: netv1.IngressRuleValue{
							HTTP: &netv1.HTTPIngressRuleValue{
								Paths: []netv1.HTTPIngressPath{
									{
										Path:     "/foobar",
										PathType: netv1PathType(netv1.PathTypePrefix),
										Backend: netv1.IngressBackend{
											Service: &netv1.IngressServiceBackend{
												Name: "myService",
												Port: netv1.ServiceBackendPort{
													Number: 443,
												},
											},
										},
									},
								},
							},
						},
					},
				},
				TLS: []netv1.IngressTLS{
					{
						Hosts:      []string{"foo.com"},
						SecretName: "mySecret",
					},
				},
			},
			Status: netv1.IngressStatus{
				LoadBalancer: corev1.LoadBalancerStatus{
					Ingress: []corev1.LoadBalancerIngress{
						{
							IP:       "1.2.3.4",
							Hostname: "foo.bar",
							Ports: []corev1.PortStatus{
								{
									Port:     8080,
									Protocol: "TCP",
								},
							},
						},
					},
				},
			},
		},
	}
	objects := loadK8sObjects(t, "fixtures/ingress/v1.18-ingress.yml")

	kubeClient := kubemock.NewSimpleClientset(objects...)

	f, err := watchAll(context.Background(), kubeClient, "v1.18")
	require.NoError(t, err)

	got, err := f.fetchIngresses()
	require.NoError(t, err)

	assert.Equal(t, want, got)
}

func Test_GetControllerType(t *testing.T) {
	tests := []struct {
		desc           string
		ingress        *netv1.Ingress
		ingressClasses []*netv1.IngressClass
		wantType       string
	}{
		{
			desc: "No IngressClassName and annotation",
			ingress: &netv1.Ingress{
				ObjectMeta: metav1.ObjectMeta{
					Annotations: map[string]string{},
				},
			},
		},
		{
			desc: "IngressClassName matching traefik controller",
			ingress: &netv1.Ingress{
				Spec: netv1.IngressSpec{
					IngressClassName: stringPtr("foo"),
				},
			},
			ingressClasses: []*netv1.IngressClass{
				{
					ObjectMeta: metav1.ObjectMeta{
						Name: "foo",
					},
					Spec: netv1.IngressClassSpec{
						Controller: ControllerTypeTraefik,
					},
				},
			},
			wantType: "traefik",
		},
		{
			desc: "IngressClassName matching unknown controller",
			ingress: &netv1.Ingress{
				Spec: netv1.IngressSpec{
					IngressClassName: stringPtr("foo"),
				},
			},
			ingressClasses: []*netv1.IngressClass{
				{
					ObjectMeta: metav1.ObjectMeta{
						Name: "foo",
					},
					Spec: netv1.IngressClassSpec{
						Controller: "my-ingress-controller",
					},
				},
			},
			wantType: "my-ingress-controller",
		},
		{
			desc: "Unknown controller with annotation",
			ingress: &netv1.Ingress{
				ObjectMeta: metav1.ObjectMeta{
					Annotations: map[string]string{
						"kubernetes.io/ingress.class": "my-ingress-class",
					},
				},
			},
			wantType: "my-ingress-class",
		},
	}

	for _, test := range tests {
		test := test
		t.Run(test.desc, func(t *testing.T) {
			t.Parallel()

			assert.Equal(t, test.wantType, getControllerType(test.ingress, test.ingressClasses))
		})
	}
}

func loadK8sObjects(t *testing.T, path string) []runtime.Object {
	t.Helper()

	content, err := os.ReadFile(path)
	require.NoError(t, err)

	files := strings.Split(string(content), "---")

	objects := make([]runtime.Object, 0, len(files))
	for _, file := range files {
		if file == "\n" || file == "" {
			continue
		}

		decoder := scheme.Codecs.UniversalDeserializer()
		object, _, err := decoder.Decode([]byte(file), nil, nil)
		require.NoError(t, err)

		objects = append(objects, object)
	}

	return objects
}

func stringPtr(s string) *string {
	return &s
}

func netv1PathType(pathType netv1.PathType) *netv1.PathType {
	return &pathType
}

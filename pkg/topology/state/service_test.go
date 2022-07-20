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
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	kubemock "k8s.io/client-go/kubernetes/fake"
	kubetesting "k8s.io/client-go/testing"
)

func TestFetcher_GetServices(t *testing.T) {
	wantSvcs := map[string]*Service{
		"myService@myns": {
			Name:      "myService",
			Namespace: "myns",
			Annotations: map[string]string{
				"my.annotation": "foo",
			},
			Type:          corev1.ServiceTypeClusterIP,
			ExternalPorts: []int{443},
		},
	}

	objects := []runtime.Object{
		&corev1.Service{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "myService",
				Namespace: "myns",
				Annotations: map[string]string{
					"my.annotation": "foo",
				},
			},
			Spec: corev1.ServiceSpec{
				Type: corev1.ServiceTypeClusterIP,
				Selector: map[string]string{
					"my.label": "foo",
				},
				Ports: []corev1.ServicePort{
					{
						Port: 443,
						Name: "https",
					},
				},
			},
			Status: corev1.ServiceStatus{
				LoadBalancer: corev1.LoadBalancerStatus{
					Ingress: []corev1.LoadBalancerIngress{
						{
							IP:       "1.2.3.4",
							Hostname: "foo.bar",
							Ports: []corev1.PortStatus{
								{
									Port:     443,
									Protocol: "TCP",
								},
							},
						},
					},
				},
			},
		},
	}

	kubeClient := kubemock.NewSimpleClientset(objects...)

	f, err := watchAll(context.Background(), kubeClient, "v1.20.1")
	require.NoError(t, err)

	gotSvcs, err := f.getServices()
	require.NoError(t, err)

	assert.Equal(t, wantSvcs, gotSvcs)
}

func TestFetcher_GetServicesWithExternalIPs(t *testing.T) {
	wantSvcs := map[string]*Service{
		"myService@myns": {
			Name:      "myService",
			Namespace: "myns",
			Annotations: map[string]string{
				"my.annotation": "foo",
			},
			Type: corev1.ServiceTypeLoadBalancer,
			ExternalIPs: []string{
				"foo.bar",
			},
			ExternalPorts: []int{443},
		},
	}

	objects := []runtime.Object{
		&corev1.Service{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "myService",
				Namespace: "myns",
				Annotations: map[string]string{
					"my.annotation": "foo",
				},
			},
			Spec: corev1.ServiceSpec{
				Type: corev1.ServiceTypeLoadBalancer,
				Selector: map[string]string{
					"my.label": "foo",
				},
				Ports: []corev1.ServicePort{
					{
						Port:     443,
						NodePort: 32085,
						Name:     "https",
					},
				},
			},
			Status: corev1.ServiceStatus{
				LoadBalancer: corev1.LoadBalancerStatus{
					Ingress: []corev1.LoadBalancerIngress{
						{
							IP:       "1.2.3.4",
							Hostname: "foo.bar",
							Ports: []corev1.PortStatus{
								{
									Port:     443,
									Protocol: "TCP",
								},
							},
						},
					},
				},
			},
		},
	}

	kubeClient := kubemock.NewSimpleClientset(objects...)

	f, err := watchAll(context.Background(), kubeClient, "v1.20.1")
	require.NoError(t, err)

	gotSvcs, err := f.getServices()
	require.NoError(t, err)

	assert.Equal(t, wantSvcs, gotSvcs)
}

func TestFetcher_GetServiceLogs(t *testing.T) {
	objects := []runtime.Object{
		&corev1.Service{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "myService",
				Namespace: "myns",
			},
			Spec: corev1.ServiceSpec{
				Type: corev1.ServiceTypeClusterIP,
				Selector: map[string]string{
					"my.label": "foo",
				},
				Ports: []corev1.ServicePort{
					{
						Port: 443,
						Name: "https",
					},
				},
			},
			Status: corev1.ServiceStatus{
				LoadBalancer: corev1.LoadBalancerStatus{
					Ingress: []corev1.LoadBalancerIngress{
						{
							IP:       "1.2.3.4",
							Hostname: "foo.bar",
							Ports: []corev1.PortStatus{
								{
									Port:     443,
									Protocol: "TCP",
								},
							},
						},
					},
				},
			},
		},
		&corev1.Pod{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "pod1",
				Namespace: "myns",
				Labels: map[string]string{
					"my.label": "foo",
				},
			},
		},
		&corev1.Pod{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "pod2",
				Namespace: "myns",
				Labels: map[string]string{
					"my.label": "foo",
				},
			},
		},
	}

	kubeClient := kubemock.NewSimpleClientset(objects...)
	kubeClient.PrependReactor("get", "pods", func(action kubetesting.Action) (handled bool, ret runtime.Object, err error) {
		if action.GetSubresource() != "log" {
			return false, nil, nil
		}
		return true, nil, nil
	})

	f, err := watchAll(context.Background(), kubeClient, "v1.20.1")
	require.NoError(t, err)

	got, err := f.GetServiceLogs(context.Background(), "myns", "myService", 20, 200)
	require.NoError(t, err)

	assert.Equal(t, []byte("fake logs\nfake logs\n"), got)
}

func TestFetcher_GetServiceLogsHandlesTooManyPods(t *testing.T) {
	objects := []runtime.Object{
		&corev1.Service{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "myService",
				Namespace: "myns",
			},
			Spec: corev1.ServiceSpec{
				Type: corev1.ServiceTypeClusterIP,
				Selector: map[string]string{
					"my.label": "foo",
				},
				Ports: []corev1.ServicePort{
					{
						Port: 443,
						Name: "https",
					},
				},
			},
			Status: corev1.ServiceStatus{
				LoadBalancer: corev1.LoadBalancerStatus{
					Ingress: []corev1.LoadBalancerIngress{
						{
							IP:       "1.2.3.4",
							Hostname: "foo.bar",
							Ports: []corev1.PortStatus{
								{
									Port:     443,
									Protocol: "TCP",
								},
							},
						},
					},
				},
			},
		},
		&corev1.Pod{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "pod1",
				Namespace: "myns",
				Labels: map[string]string{
					"my.label": "foo",
				},
			},
		},
		&corev1.Pod{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "pod2",
				Namespace: "myns",
				Labels: map[string]string{
					"my.label": "foo",
				},
			},
		},
		&corev1.Pod{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "pod3",
				Namespace: "myns",
				Labels: map[string]string{
					"my.label": "foo",
				},
			},
		},
	}

	kubeClient := kubemock.NewSimpleClientset(objects...)
	kubeClient.PrependReactor("get", "pods", func(action kubetesting.Action) (handled bool, ret runtime.Object, err error) {
		if action.GetSubresource() != "log" {
			return false, nil, nil
		}
		return true, nil, nil
	})

	f, err := watchAll(context.Background(), kubeClient, "v1.20.1")
	require.NoError(t, err)

	got, err := f.GetServiceLogs(context.Background(), "myns", "myService", 2, 200)
	require.NoError(t, err)

	assert.Equal(t, []byte("fake logs\nfake logs\n"), got)
}

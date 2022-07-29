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

package reviewer

import (
	"context"
	"encoding/json"
	"errors"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/traefik/hub-agent-kubernetes/pkg/acp"
	"github.com/traefik/hub-agent-kubernetes/pkg/acp/admission/ingclass"
	"github.com/traefik/hub-agent-kubernetes/pkg/acp/basicauth"
	"github.com/traefik/hub-agent-kubernetes/pkg/acp/jwt"
	"github.com/traefik/hub-agent-kubernetes/pkg/acp/oidc"
	traefikv1alpha1 "github.com/traefik/hub-agent-kubernetes/pkg/crd/api/traefik/v1alpha1"
	traefikkubemock "github.com/traefik/hub-agent-kubernetes/pkg/crd/generated/client/traefik/clientset/versioned/fake"
	admv1 "k8s.io/api/admission/v1"
	netv1 "k8s.io/api/networking/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
)

func TestTraefikIngress_CanReviewChecksKind(t *testing.T) {
	ingClasses := newIngressClassesMock(t)
	ingClasses.OnGetDefaultController().TypedReturns(ingclass.ControllerTypeTraefik, nil)

	tests := []struct {
		desc      string
		kind      metav1.GroupVersionKind
		canReview bool
	}{
		{
			desc: "can review networking.k8s.io v1 Ingresses",
			kind: metav1.GroupVersionKind{
				Group:   "networking.k8s.io",
				Version: "v1",
				Kind:    "Ingress",
			},
			canReview: true,
		},
		{
			desc: "can't review invalid networking.k8s.io Ingress version",
			kind: metav1.GroupVersionKind{
				Group:   "networking.k8s.io",
				Version: "invalid",
				Kind:    "Ingress",
			},
			canReview: false,
		},
		{
			desc: "can't review invalid networking.k8s.io Ingress group",
			kind: metav1.GroupVersionKind{
				Group:   "invalid",
				Version: "v1",
				Kind:    "Ingress",
			},
			canReview: false,
		},
		{
			desc: "can't review non Ingress networking.k8s.io v1 resources",
			kind: metav1.GroupVersionKind{
				Group:   "networking.k8s.io",
				Version: "v1",
				Kind:    "NetworkPolicy",
			},
			canReview: false,
		},
		{
			desc: "can review extensions v1beta1 Ingresses",
			kind: metav1.GroupVersionKind{
				Group:   "extensions",
				Version: "v1beta1",
				Kind:    "Ingress",
			},
			canReview: true,
		},
		{
			desc: "can't review invalid extensions Ingress version",
			kind: metav1.GroupVersionKind{
				Group:   "extensions",
				Version: "invalid",
				Kind:    "Ingress",
			},
			canReview: false,
		},
		{
			desc: "can't review invalid v1beta1 Ingress group",
			kind: metav1.GroupVersionKind{
				Group:   "invalid",
				Version: "v1beta1",
				Kind:    "Ingress",
			},
			canReview: false,
		},
		{
			desc: "can't review invalid extension v1beta1 resource",
			kind: metav1.GroupVersionKind{
				Group:   "extensions",
				Version: "v1beta1",
				Kind:    "Invalid",
			},
			canReview: false,
		},
	}

	for _, test := range tests {
		test := test
		t.Run(test.desc, func(t *testing.T) {
			t.Parallel()

			fwdAuthMdlwrs := NewFwdAuthMiddlewares("", nil, nil)
			review := NewTraefikIngress(ingClasses, fwdAuthMdlwrs)

			var ing netv1.Ingress
			b, err := json.Marshal(ing)
			require.NoError(t, err)

			ar := admv1.AdmissionReview{
				Request: &admv1.AdmissionRequest{
					Kind:   test.kind,
					Object: runtime.RawExtension{Raw: b},
				},
			}

			ok, err := review.CanReview(ar)
			require.NoError(t, err)
			assert.Equal(t, test.canReview, ok)
		})
	}
}

func TestTraefikIngress_CanReviewChecksIngressClass(t *testing.T) {
	tests := []struct {
		desc               string
		annotation         string
		spec               string
		ingressClassesMock func(t *testing.T) IngressClasses
		canReview          assert.BoolAssertionFunc
		canReviewErr       assert.ErrorAssertionFunc
	}{
		{
			desc: "can review a valid resource",
			ingressClassesMock: func(t *testing.T) IngressClasses {
				t.Helper()

				return newIngressClassesMock(t).
					OnGetDefaultController().TypedReturns(ingclass.ControllerTypeTraefik, nil).Once().
					Parent
			},
			canReview:    assert.True,
			canReviewErr: assert.NoError,
		},
		{
			desc: "can't review if the default controller is not of the correct type",
			ingressClassesMock: func(t *testing.T) IngressClasses {
				t.Helper()

				return newIngressClassesMock(t).
					OnGetDefaultController().TypedReturns("nope", nil).Once().
					Parent
			},
			canReview:    assert.False,
			canReviewErr: assert.NoError,
		},
		{
			desc:       "can review if annotation is correct",
			annotation: "traefik",
			ingressClassesMock: func(t *testing.T) IngressClasses {
				t.Helper()

				return newIngressClassesMock(t).
					OnGetDefaultController().TypedReturns(ingclass.ControllerTypeTraefik, nil).Once().
					Parent
			},
			canReview:    assert.True,
			canReviewErr: assert.NoError,
		},
		{
			desc:       "can review if using a custom ingress class (annotation)",
			annotation: "custom-traefik-ingress-class",
			ingressClassesMock: func(t *testing.T) IngressClasses {
				t.Helper()

				return newIngressClassesMock(t).
					OnGetDefaultController().TypedReturns(ingclass.ControllerTypeTraefik, nil).Once().
					OnGetController("custom-traefik-ingress-class").TypedReturns(ingclass.ControllerTypeTraefik, nil).Once().
					Parent
			},
			canReview:    assert.True,
			canReviewErr: assert.NoError,
		},
		{
			desc: "can review if using a custom ingress class (spec)",
			spec: "custom-traefik-ingress-class",
			ingressClassesMock: func(t *testing.T) IngressClasses {
				t.Helper()

				return newIngressClassesMock(t).
					OnGetDefaultController().TypedReturns(ingclass.ControllerTypeTraefik, nil).Once().
					OnGetController("custom-traefik-ingress-class").TypedReturns(ingclass.ControllerTypeTraefik, nil).Once().
					Parent
			},
			canReview:    assert.True,
			canReviewErr: assert.NoError,
		},
		{
			desc: "can't review if using another controller",
			spec: "powpow",
			ingressClassesMock: func(t *testing.T) IngressClasses {
				t.Helper()

				return newIngressClassesMock(t).
					OnGetDefaultController().TypedReturns(ingclass.ControllerTypeTraefik, nil).Once().
					OnGetController("powpow").TypedReturns("", errors.New("nope")).Once().
					Parent
			},
			canReview:    assert.False,
			canReviewErr: assert.Error,
		},
		{
			desc:       "spec takes priority over annotation#1",
			annotation: "powpow",
			spec:       "custom-traefik-ingress-class",
			ingressClassesMock: func(t *testing.T) IngressClasses {
				t.Helper()

				return newIngressClassesMock(t).
					OnGetDefaultController().TypedReturns(ingclass.ControllerTypeTraefik, nil).Once().
					OnGetController("custom-traefik-ingress-class").TypedReturns(ingclass.ControllerTypeTraefik, nil).Once().
					Parent
			},
			canReview:    assert.True,
			canReviewErr: assert.NoError,
		},
		{
			desc:       "spec takes priority over annotation#2",
			annotation: "traefik",
			spec:       "powpow",
			ingressClassesMock: func(t *testing.T) IngressClasses {
				t.Helper()

				return newIngressClassesMock(t).
					OnGetDefaultController().TypedReturns(ingclass.ControllerTypeTraefik, nil).Once().
					OnGetController("powpow").TypedReturns("", errors.New("nope")).Once().
					Parent
			},
			canReview:    assert.False,
			canReviewErr: assert.Error,
		},
	}

	for _, test := range tests {
		test := test
		t.Run(test.desc, func(t *testing.T) {
			t.Parallel()

			fwdAuthMdlwrs := NewFwdAuthMiddlewares("", nil, nil)
			review := NewTraefikIngress(test.ingressClassesMock(t), fwdAuthMdlwrs)

			ing := netv1.Ingress{
				ObjectMeta: metav1.ObjectMeta{
					Annotations: map[string]string{
						"kubernetes.io/ingress.class": test.annotation,
					},
				},
				Spec: netv1.IngressSpec{
					IngressClassName: &test.spec,
				},
			}

			b, err := json.Marshal(ing)
			require.NoError(t, err)

			ar := admv1.AdmissionReview{
				Request: &admv1.AdmissionRequest{
					Kind: metav1.GroupVersionKind{
						Group:   "networking.k8s.io",
						Version: "v1",
						Kind:    "Ingress",
					},
					Object: runtime.RawExtension{
						Raw: b,
					},
				},
			}

			ok, err := review.CanReview(ar)
			test.canReviewErr(t, err)
			test.canReview(t, ok)
		})
	}
}

func TestTraefikIngress_ReviewAddsAuthentication(t *testing.T) {
	tests := []struct {
		desc                    string
		config                  *acp.Config
		oldIngAnno              map[string]string
		ingAnno                 map[string]string
		wantPatch               map[string]string
		wantAuthResponseHeaders []string
	}{
		{
			desc: "add JWT authentication",
			config: &acp.Config{JWT: &jwt.Config{
				ForwardHeaders: map[string]string{
					"fwdHeader": "claim",
				},
			}},
			oldIngAnno: map[string]string{
				AnnotationHubAuth:   "my-old-policy@test",
				"custom-annotation": "foobar",
				"traefik.ingress.kubernetes.io/router.middlewares": "test-zz-my-old-policy-test@kubernetescrd",
			},
			ingAnno: map[string]string{
				AnnotationHubAuth:   "my-policy@test",
				"custom-annotation": "foobar",
				"traefik.ingress.kubernetes.io/router.middlewares": "custom-middleware@kubernetescrd",
			},
			wantPatch: map[string]string{
				AnnotationHubAuth:   "my-policy@test",
				"custom-annotation": "foobar",
				"traefik.ingress.kubernetes.io/router.middlewares": "custom-middleware@kubernetescrd,test-zz-my-policy-test@kubernetescrd",
			},
			wantAuthResponseHeaders: []string{"fwdHeader"},
		},
		{
			desc: "add Basic authentication",
			config: &acp.Config{BasicAuth: &basicauth.Config{
				StripAuthorizationHeader: true,
				ForwardUsernameHeader:    "User",
			}},
			oldIngAnno: map[string]string{},
			ingAnno: map[string]string{
				AnnotationHubAuth:   "my-policy@test",
				"custom-annotation": "foobar",
				"traefik.ingress.kubernetes.io/router.middlewares": "custom-middleware@kubernetescrd",
			},
			wantPatch: map[string]string{
				AnnotationHubAuth:   "my-policy@test",
				"custom-annotation": "foobar",
				"traefik.ingress.kubernetes.io/router.middlewares": "custom-middleware@kubernetescrd,test-zz-my-policy-test@kubernetescrd",
			},
			wantAuthResponseHeaders: []string{"User", "Authorization"},
		},
		{
			desc: "add OIDC authentication",
			config: &acp.Config{OIDC: &oidc.Config{
				ForwardHeaders: map[string]string{
					"fwdHeader": "claim",
				},
			}},
			oldIngAnno: map[string]string{},
			ingAnno: map[string]string{
				AnnotationHubAuth:   "my-policy@test",
				"custom-annotation": "foobar",
				"traefik.ingress.kubernetes.io/router.middlewares": "custom-middleware@kubernetescrd",
			},
			wantPatch: map[string]string{
				AnnotationHubAuth:   "my-policy@test",
				"custom-annotation": "foobar",
				"traefik.ingress.kubernetes.io/router.middlewares": "custom-middleware@kubernetescrd,test-zz-my-policy-test@kubernetescrd",
			},
			wantAuthResponseHeaders: []string{"fwdHeader", "Set-Cookie"},
		},
	}

	for _, test := range tests {
		test := test
		t.Run(test.desc, func(t *testing.T) {
			t.Parallel()

			traefikClientSet := traefikkubemock.NewSimpleClientset()

			policies := newPolicyGetterMock(t)
			policies.OnGetConfig("my-policy@test").TypedReturns(test.config, nil).Once()

			fwdAuthMdlwrs := NewFwdAuthMiddlewares("", policies, traefikClientSet.TraefikV1alpha1())

			rev := NewTraefikIngress(newIngressClassesMock(t), fwdAuthMdlwrs)

			oldIng := struct {
				Metadata metav1.ObjectMeta `json:"metadata"`
			}{
				Metadata: metav1.ObjectMeta{
					Name:        "name",
					Namespace:   "test",
					Annotations: test.oldIngAnno,
				},
			}
			oldB, err := json.Marshal(oldIng)
			require.NoError(t, err)

			ing := struct {
				Metadata metav1.ObjectMeta `json:"metadata"`
			}{
				Metadata: metav1.ObjectMeta{
					Name:        "name",
					Namespace:   "test",
					Annotations: test.ingAnno,
				},
			}
			b, err := json.Marshal(ing)
			require.NoError(t, err)

			ar := admv1.AdmissionReview{
				Request: &admv1.AdmissionRequest{
					Object: runtime.RawExtension{
						Raw: b,
					},
					OldObject: runtime.RawExtension{
						Raw: oldB,
					},
				},
			}

			patch, err := rev.Review(context.Background(), ar)
			assert.NoError(t, err)
			assert.NotNil(t, patch)

			assert.Equal(t, 3, len(patch))
			assert.Equal(t, "replace", patch["op"])
			assert.Equal(t, "/metadata/annotations", patch["path"])
			assert.Equal(t, len(test.wantPatch), len(patch["value"].(map[string]string)))
			for k := range test.wantPatch {
				assert.Equal(t, test.wantPatch[k], patch["value"].(map[string]string)[k])
			}

			m, err := traefikClientSet.TraefikV1alpha1().Middlewares("test").
				Get(context.Background(), "zz-my-policy-test", metav1.GetOptions{})
			assert.NoError(t, err)
			assert.NotNil(t, m)

			assert.Equal(t, test.wantAuthResponseHeaders, m.Spec.ForwardAuth.AuthResponseHeaders)
		})
	}
}

func TestTraefikIngress_ReviewUpdatesExistingMiddleware(t *testing.T) {
	tests := []struct {
		desc                    string
		config                  *acp.Config
		wantAuthResponseHeaders []string
	}{
		{
			desc: "Update middleware with JWT configuration",
			config: &acp.Config{
				JWT: &jwt.Config{
					StripAuthorizationHeader: true,
				},
			},
			wantAuthResponseHeaders: []string{"Authorization"},
		},
		{
			desc: "Update middleware with basic configuration",
			config: &acp.Config{
				BasicAuth: &basicauth.Config{
					StripAuthorizationHeader: true,
				},
			},
			wantAuthResponseHeaders: []string{"Authorization"},
		},
	}

	for _, test := range tests {
		test := test
		t.Run(test.desc, func(t *testing.T) {
			t.Parallel()

			middleware := traefikv1alpha1.Middleware{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "zz-my-policy-test",
					Namespace: "test",
				},
				Spec: traefikv1alpha1.MiddlewareSpec{
					ForwardAuth: &traefikv1alpha1.ForwardAuth{
						AuthResponseHeaders: []string{"fwdHeader"},
					},
				},
			}

			traefikClientSet := traefikkubemock.NewSimpleClientset(&middleware)

			policies := newPolicyGetterMock(t)
			policies.OnGetConfig("my-policy@test").TypedReturns(test.config, nil).Once()

			fwdAuthMdlwrs := NewFwdAuthMiddlewares("", policies, traefikClientSet.TraefikV1alpha1())
			rev := NewTraefikIngress(newIngressClassesMock(t), fwdAuthMdlwrs)

			ing := struct {
				Metadata metav1.ObjectMeta `json:"metadata"`
			}{
				Metadata: metav1.ObjectMeta{
					Name:        "name",
					Namespace:   "test",
					Annotations: map[string]string{AnnotationHubAuth: "my-policy@test"},
				},
			}
			b, err := json.Marshal(ing)
			require.NoError(t, err)

			ar := admv1.AdmissionReview{
				Request: &admv1.AdmissionRequest{
					Object: runtime.RawExtension{Raw: b},
				},
			}

			m, err := traefikClientSet.TraefikV1alpha1().Middlewares("test").
				Get(context.Background(), "zz-my-policy-test", metav1.GetOptions{})
			assert.NoError(t, err)
			assert.NotNil(t, m)
			assert.Equal(t, []string{"fwdHeader"}, m.Spec.ForwardAuth.AuthResponseHeaders)

			p, err := rev.Review(context.Background(), ar)
			assert.NoError(t, err)
			assert.NotNil(t, p)

			m, err = traefikClientSet.TraefikV1alpha1().Middlewares("test").
				Get(context.Background(), "zz-my-policy-test", metav1.GetOptions{})
			assert.NoError(t, err)
			assert.NotNil(t, m)

			assert.Equal(t, test.wantAuthResponseHeaders, m.Spec.ForwardAuth.AuthResponseHeaders)
		})
	}
}

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

package acp

import (
	"context"
	"fmt"
	"reflect"
	"time"

	"github.com/rs/zerolog/log"
	hubv1alpha1 "github.com/traefik/hub-agent-kubernetes/pkg/crd/api/hub/v1alpha1"
	hubclientset "github.com/traefik/hub-agent-kubernetes/pkg/crd/generated/client/hub/clientset/versioned"
	hubinformer "github.com/traefik/hub-agent-kubernetes/pkg/crd/generated/client/hub/informers/externalversions"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
)

// Client for the ACP service.
type Client interface {
	GetACPs(ctx context.Context) ([]ACP, error)
}

// ACP is the Access Control Policy retrieved from the platform.
type ACP struct {
	Config

	ID      string `json:"id"`
	Name    string `json:"name"`
	Version string `json:"version"`
}

// Watcher watches hub ACPs.
type Watcher struct {
	interval     time.Duration
	client       Client
	hubClientSet hubclientset.Interface
	hubInformer  hubinformer.SharedInformerFactory
}

// NewWatcher returns a new Watcher.
func NewWatcher(interval time.Duration, client Client, hubClientSet hubclientset.Interface, hubInformer hubinformer.SharedInformerFactory) *Watcher {
	return &Watcher{
		interval:     interval,
		client:       client,
		hubClientSet: hubClientSet,
		hubInformer:  hubInformer,
	}
}

// Run runs Watcher.
func (w *Watcher) Run(ctx context.Context) {
	t := time.NewTicker(w.interval)
	defer t.Stop()

	for {
		select {
		case <-ctx.Done():
			log.Info().Msg("Stopping ACP watcher")
			return
		case <-t.C:
			ctxFetch, cancel := context.WithTimeout(ctx, 5*time.Second)
			acps, err := w.client.GetACPs(ctxFetch)
			if err != nil {
				log.Error().Err(err).Msg("Fetching ACPs")
				cancel()
				continue
			}
			cancel()

			policies, err := w.hubInformer.Hub().V1alpha1().AccessControlPolicies().Lister().List(labels.Everything())
			if err != nil {
				log.Error().Err(err).Msg("Listing ACPs")
				continue
			}

			policiesByID := map[string]*hubv1alpha1.AccessControlPolicy{}
			for _, p := range policies {
				policiesByID[p.Name] = p
			}

			for _, a := range acps {
				policy, found := policiesByID[a.Name]
				// We delete the policy from the map, since we use this map to delete unused policies.
				delete(policiesByID, a.Name)

				if found && !needUpdate(a, policy) {
					continue
				}

				if !found {
					if err := w.createPolicy(ctx, a); err != nil {
						log.Error().Err(err).Str("name", a.Name).Msg("Creating ACP")
					}
					continue
				}

				policy.Spec = buildAccessControlPolicySpec(a)
				policy.Status.Version = a.Version

				var err error
				policy.Status.SpecHash, err = policy.Spec.Hash()
				if err != nil {
					log.Error().Err(err).Str("name", policy.Name).Msg("Build spec hash")
					continue
				}
				if err := w.updatePolicy(ctx, policy); err != nil {
					log.Error().Err(err).Str("name", policy.Name).Msg("Upsert ACP")
				}
			}

			w.cleanPolicies(ctx, policiesByID)
		}
	}
}

func (w *Watcher) createPolicy(ctx context.Context, acp ACP) error {
	policy := &hubv1alpha1.AccessControlPolicy{
		ObjectMeta: metav1.ObjectMeta{
			Name: acp.Name,
		},
		Status: hubv1alpha1.AccessControlPolicyStatus{
			Version: acp.Version,
		},
	}
	policy.Spec = buildAccessControlPolicySpec(acp)

	var err error
	policy.Status.SpecHash, err = policy.Spec.Hash()
	if err != nil {
		return fmt.Errorf("build spec hash: %w ", err)
	}

	ctxCreate, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()

	if _, err := w.hubClientSet.HubV1alpha1().AccessControlPolicies().Create(ctxCreate, policy, metav1.CreateOptions{}); err != nil {
		return fmt.Errorf("creating ACP: %w", err)
	}
	log.Debug().Str("name", policy.Name).Msg("ACP created")
	return nil
}

func (w *Watcher) updatePolicy(ctx context.Context, policy *hubv1alpha1.AccessControlPolicy) error {
	ctxUpdate, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()

	if _, err := w.hubClientSet.HubV1alpha1().AccessControlPolicies().Update(ctxUpdate, policy, metav1.UpdateOptions{}); err != nil {
		return fmt.Errorf("updating ACP: %w", err)
	}
	log.Debug().Str("name", policy.Name).Msg("ACP updated")

	return nil
}

func (w *Watcher) cleanPolicies(ctx context.Context, policies map[string]*hubv1alpha1.AccessControlPolicy) {
	for _, p := range policies {
		ctxDelete, cancel := context.WithTimeout(ctx, 5*time.Second)
		err := w.hubClientSet.HubV1alpha1().AccessControlPolicies().Delete(ctxDelete, p.Name, metav1.DeleteOptions{})
		if err != nil {
			log.Error().Err(err).Msg("Deleting ACP")
			cancel()
			continue
		}
		log.Debug().Str("name", p.Name).Msg("ACP deleted")
		cancel()
	}
}

func needUpdate(a ACP, policy *hubv1alpha1.AccessControlPolicy) bool {
	return !reflect.DeepEqual(buildAccessControlPolicySpec(a), policy.Spec)
}

func buildAccessControlPolicySpec(a ACP) hubv1alpha1.AccessControlPolicySpec {
	spec := hubv1alpha1.AccessControlPolicySpec{}
	switch {
	case a.OIDC != nil:
		spec.OIDC = &hubv1alpha1.AccessControlOIDC{
			Issuer:       a.OIDC.Issuer,
			ClientID:     a.OIDC.ClientID,
			ClientSecret: a.OIDC.ClientSecret,
			RedirectURL:  a.OIDC.RedirectURL,
			LogoutURL:    a.OIDC.LogoutURL,
			AuthParams:   a.OIDC.AuthParams,
			StateCookie: hubv1alpha1.StateCookie{
				Secret:   a.OIDC.StateCookie.Secret,
				SameSite: a.OIDC.StateCookie.SameSite,
				Secure:   a.OIDC.StateCookie.Secure,
				Domain:   a.OIDC.StateCookie.Domain,
				Path:     a.OIDC.StateCookie.Path,
			},
			Session: hubv1alpha1.Session{
				Secret:   a.OIDC.Session.Secret,
				SameSite: a.OIDC.Session.SameSite,
				Secure:   a.OIDC.Session.Secure,
				Domain:   a.OIDC.Session.Domain,
				Path:     a.OIDC.Session.Path,
				Refresh:  a.OIDC.Session.Refresh,
			},
			Scopes:         a.OIDC.Scopes,
			ForwardHeaders: a.OIDC.ForwardHeaders,
			Claims:         a.OIDC.Claims,
		}
		if a.OIDC.TLS != nil {
			spec.OIDC.TLS = &hubv1alpha1.TLS{
				CABundle:           a.OIDC.TLS.CABundle,
				InsecureSkipVerify: a.OIDC.TLS.InsecureSkipVerify,
			}
		}
	case a.JWT != nil:
		spec.JWT = &hubv1alpha1.AccessControlPolicyJWT{
			SigningSecret:              a.JWT.SigningSecret,
			SigningSecretBase64Encoded: a.JWT.SigningSecretBase64Encoded,
			PublicKey:                  a.JWT.PublicKey,
			JWKsFile:                   a.JWT.JWKsFile.String(),
			JWKsURL:                    a.JWT.JWKsURL,
			StripAuthorizationHeader:   a.JWT.StripAuthorizationHeader,
			ForwardHeaders:             a.JWT.ForwardHeaders,
			TokenQueryKey:              a.JWT.TokenQueryKey,
			Claims:                     a.JWT.Claims,
		}

	case a.BasicAuth != nil:
		spec.BasicAuth = &hubv1alpha1.AccessControlPolicyBasicAuth{
			Users:                    a.BasicAuth.Users,
			Realm:                    a.BasicAuth.Realm,
			StripAuthorizationHeader: a.BasicAuth.StripAuthorizationHeader,
			ForwardUsernameHeader:    a.BasicAuth.ForwardUsernameHeader,
		}
	}

	return spec
}

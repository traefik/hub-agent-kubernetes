package admission

import (
	"context"
	"fmt"

	"github.com/rs/zerolog/log"
	"github.com/traefik/neo-agent/pkg/acp"
	"github.com/traefik/neo-agent/pkg/acp/admission/reviewer"
	"github.com/traefik/neo-agent/pkg/kubevers"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/client-go/informers"
	clientset "k8s.io/client-go/kubernetes"
)

// IngressUpdater handles ingress updates when ACP configurations are modified.
type IngressUpdater struct {
	informer  informers.SharedInformerFactory
	clientSet clientset.Interface

	cancelUpd map[string]context.CancelFunc

	polNameCh chan string

	supportsNetV1Ingresses bool
}

// NewIngressUpdater return a new IngressUpdater.
func NewIngressUpdater(informer informers.SharedInformerFactory, clientSet clientset.Interface, kubeVersion string) *IngressUpdater {
	return &IngressUpdater{
		informer:               informer,
		clientSet:              clientSet,
		cancelUpd:              map[string]context.CancelFunc{},
		polNameCh:              make(chan string),
		supportsNetV1Ingresses: kubevers.SupportsNetV1Ingresses(kubeVersion),
	}
}

// Run runs the IngressUpdater control loop, updating ingress resources when needed.
func (u *IngressUpdater) Run(ctx context.Context) {
	for {
		select {
		case polName := <-u.polNameCh:
			if cancel, ok := u.cancelUpd[polName]; ok {
				cancel()
				delete(u.cancelUpd, polName)
			}

			ctxUpd, cancel := context.WithCancel(ctx)
			u.cancelUpd[polName] = cancel

			go func(polName string) {
				err := u.updateIngresses(ctxUpd, polName)
				if err != nil {
					log.Error().Err(err).Str("acp_name", polName).Msg("Unable to update ingresses")
				}
			}(polName)

		case <-ctx.Done():
			return
		}
	}
}

// Update notifies the IngressUpdater control loop that it should update ingresses referencing the given ACP if they had
// a header-related configuration change.
func (u *IngressUpdater) Update(polName string) {
	u.polNameCh <- polName
}

func (u *IngressUpdater) updateIngresses(ctx context.Context, polName string) error {
	if !u.supportsNetV1Ingresses {
		return u.updateV1beta1Ingresses(ctx, polName)
	}

	return u.updateV1Ingresses(ctx, polName)
}

func (u *IngressUpdater) updateV1Ingresses(ctx context.Context, polName string) error {
	ingList, err := u.informer.Networking().V1().Ingresses().Lister().List(labels.Everything())
	if err != nil {
		return fmt.Errorf("list ingresses: %w", err)
	}

	log.Debug().Int("ingress_number", len(ingList)).Msg("Updating ingresses")

	for _, ing := range ingList {
		// Don't continue if the context was canceled to prevent being spammed
		// with context canceled errors on every request we would send otherwise.
		select {
		case <-ctx.Done():
			return nil
		default:
		}

		var ok bool
		ok, err = shouldUpdate(ing.Annotations[reviewer.AnnotationNeoAuth], ing.Namespace, polName)
		if err != nil {
			log.Error().Err(err).Str("ingress_name", ing.Name).Str("ingress_namespace", ing.Namespace).Msg("Unable to determine if ingress should be updated")
			continue
		}
		if !ok {
			continue
		}

		_, err = u.clientSet.NetworkingV1().Ingresses(ing.Namespace).Update(ctx, ing, metav1.UpdateOptions{FieldManager: "neo-auth"})
		if err != nil {
			log.Error().Err(err).Str("ingress_name", ing.Name).Str("ingress_namespace", ing.Namespace).Msg("Unable to update ingress")
			continue
		}
	}
	return nil
}

func (u *IngressUpdater) updateV1beta1Ingresses(ctx context.Context, polName string) error {
	// As the minimum supported version is 1.14, we don't need to support the extension group.
	ingList, err := u.informer.Networking().V1beta1().Ingresses().Lister().List(labels.Everything())
	if err != nil {
		return fmt.Errorf("list legacy ingresses: %w", err)
	}

	log.Debug().Int("ingress_number", len(ingList)).Msg("Updating legacy ingresses")

	for _, ing := range ingList {
		// Don't continue if the context was canceled to prevent being spammed
		// with context canceled errors on every request we would send otherwise.
		select {
		case <-ctx.Done():
			return nil
		default:
		}

		var ok bool
		ok, err = shouldUpdate(ing.Annotations[reviewer.AnnotationNeoAuth], ing.Namespace, polName)
		if err != nil {
			log.Error().Err(err).Str("ingress_name", ing.Name).Str("ingress_namespace", ing.Namespace).Msg("Unable to determine if legacy ingress should be updated")
			continue
		}
		if !ok {
			continue
		}

		_, err = u.clientSet.NetworkingV1beta1().Ingresses(ing.Namespace).Update(ctx, ing, metav1.UpdateOptions{FieldManager: "neo-auth"})
		if err != nil {
			log.Error().Err(err).Str("ingress_name", ing.Name).Str("ingress_namespace", ing.Namespace).Msg("Unable to update legacy ingress")
			continue
		}
	}
	return nil
}

func shouldUpdate(neoAuthAnno, ns, polName string) (bool, error) {
	if neoAuthAnno == "" {
		return false, nil
	}

	name, err := acp.CanonicalName(neoAuthAnno, ns)
	if err != nil {
		return false, err
	}

	if name != polName {
		return false, nil
	}

	return true, nil
}

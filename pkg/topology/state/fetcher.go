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
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/hashicorp/go-version"
	"github.com/rs/zerolog/log"
	traefikv1alpha1 "github.com/traefik/hub-agent-kubernetes/pkg/crd/api/traefik/v1alpha1"
	hubclientset "github.com/traefik/hub-agent-kubernetes/pkg/crd/generated/client/hub/clientset/versioned"
	hubinformer "github.com/traefik/hub-agent-kubernetes/pkg/crd/generated/client/hub/informers/externalversions"
	traefikclientset "github.com/traefik/hub-agent-kubernetes/pkg/crd/generated/client/traefik/clientset/versioned"
	traefikinformer "github.com/traefik/hub-agent-kubernetes/pkg/crd/generated/client/traefik/informers/externalversions"
	"github.com/traefik/hub-agent-kubernetes/pkg/kube"
	"github.com/traefik/hub-agent-kubernetes/pkg/kubevers"
	kerror "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/client-go/discovery"
	"k8s.io/client-go/informers"
	clientset "k8s.io/client-go/kubernetes"
)

// Fetcher fetches Kubernetes resources and converts them into a filtered and simplified state.
type Fetcher struct {
	clusterID     string
	serverVersion string

	k8s       informers.SharedInformerFactory
	hub       hubinformer.SharedInformerFactory
	traefik   traefikinformer.SharedInformerFactory
	clientSet clientset.Interface
}

// NewFetcher creates a new Fetcher.
func NewFetcher(ctx context.Context, clusterID string) (*Fetcher, error) {
	config, err := kube.InClusterConfigWithRetrier(2)
	if err != nil {
		return nil, fmt.Errorf("create Kubernetes in-cluster configuration: %w", err)
	}

	clientSet, err := clientset.NewForConfig(config)
	if err != nil {
		return nil, err
	}

	// TODO: Handle ingressClasses from Hub as well.
	hubClientSet, err := hubclientset.NewForConfig(config)
	if err != nil {
		return nil, err
	}

	traefikClientSet, err := traefikclientset.NewForConfig(config)
	if err != nil {
		return nil, err
	}

	serverVersion, err := clientSet.Discovery().ServerVersion()
	if err != nil {
		return nil, fmt.Errorf("get server version: %w", err)
	}

	return watchAll(ctx, clientSet, hubClientSet, traefikClientSet, serverVersion.GitVersion, clusterID)
}

func watchAll(ctx context.Context, clientSet clientset.Interface, hubClientSet hubclientset.Interface, traefikClientSet traefikclientset.Interface, serverVersion, clusterID string) (*Fetcher, error) {
	serverSemVer, err := version.NewVersion(serverVersion)
	if err != nil {
		return nil, fmt.Errorf("parse server version: %w", err)
	}

	if serverSemVer.LessThan(version.Must(version.NewVersion("1.14"))) {
		return nil, fmt.Errorf("unsupported version: %s", serverSemVer)
	}

	kubernetesFactory := informers.NewSharedInformerFactoryWithOptions(clientSet, 5*time.Minute)

	kubernetesFactory.Apps().V1().DaemonSets().Informer()
	kubernetesFactory.Apps().V1().Deployments().Informer()
	kubernetesFactory.Apps().V1().ReplicaSets().Informer()
	kubernetesFactory.Apps().V1().StatefulSets().Informer()
	kubernetesFactory.Core().V1().Endpoints().Informer()
	kubernetesFactory.Core().V1().Namespaces().Informer()
	kubernetesFactory.Core().V1().Pods().Informer()
	kubernetesFactory.Core().V1().Services().Informer()

	if kubevers.SupportsNetV1IngressClasses(serverVersion) {
		kubernetesFactory.Networking().V1().IngressClasses().Informer()
	} else if kubevers.SupportsNetV1Beta1IngressClasses(serverVersion) {
		kubernetesFactory.Networking().V1beta1().IngressClasses().Informer()
	}

	if kubevers.SupportsNetV1Ingresses(serverVersion) {
		kubernetesFactory.Networking().V1().Ingresses().Informer()
	} else {
		// Since we only support Kubernetes v1.14 and up, we always have at least net v1beta1 Ingresses.
		kubernetesFactory.Networking().V1beta1().Ingresses().Informer()
	}

	traefikFactory := traefikinformer.NewSharedInformerFactoryWithOptions(traefikClientSet, 5*time.Minute)

	hasTraefikCRDs, err := hasTraefikCRDs(clientSet.Discovery())
	if err != nil {
		return nil, fmt.Errorf("check presence of Traefik IngressRoute, TraefikService and TLSOption CRD: %w", err)
	}
	if hasTraefikCRDs {
		traefikFactory.Traefik().V1alpha1().IngressRoutes().Informer()
		traefikFactory.Traefik().V1alpha1().TraefikServices().Informer()
		traefikFactory.Traefik().V1alpha1().TLSOptions().Informer()
	} else {
		msg := "The agent has been installed in a cluster where the Traefik Proxy CustomResourceDefinitions are not installed. " +
			"If you want to install these CustomResourceDefinitions and take advantage of them in Traefik Hub, " +
			"the agent needs to be restarted in order to load them. " +
			"Run 'kubectl -n hub-agent delete pod -l app=hub-agent,component=controller'"
		log.Info().Msg(msg)
	}

	hubFactory := hubinformer.NewSharedInformerFactoryWithOptions(hubClientSet, 5*time.Minute)
	hubFactory.Hub().V1alpha1().AccessControlPolicies().Informer()

	kubernetesFactory.Start(ctx.Done())
	hubFactory.Start(ctx.Done())
	traefikFactory.Start(ctx.Done())

	for typ, ok := range kubernetesFactory.WaitForCacheSync(ctx.Done()) {
		if !ok {
			return nil, fmt.Errorf("timed out waiting for k8s object caches to sync %s", typ)
		}
	}

	for typ, ok := range hubFactory.WaitForCacheSync(ctx.Done()) {
		if !ok {
			return nil, fmt.Errorf("timed out waiting for access control policies caches to sync %s", typ)
		}
	}

	for typ, ok := range traefikFactory.WaitForCacheSync(ctx.Done()) {
		if !ok {
			return nil, fmt.Errorf("timed out waiting for Traefik CRD caches to sync %s", typ)
		}
	}

	return &Fetcher{
		clusterID:     clusterID,
		serverVersion: serverVersion,
		k8s:           kubernetesFactory,
		hub:           hubFactory,
		traefik:       traefikFactory,
		clientSet:     clientSet,
	}, nil
}

// FetchState assembles a cluster state from Kubernetes resources.
func (f *Fetcher) FetchState() (*Cluster, error) {
	cluster := &Cluster{
		ID: f.clusterID,
	}

	var err error

	cluster.Namespaces, err = f.getNamespaces()
	if err != nil {
		return nil, err
	}

	cluster.Apps, err = f.getApps()
	if err != nil {
		return nil, err
	}

	cluster.TLSOptions, err = f.getTLSOptions()
	if err != nil {
		return nil, err
	}

	cluster.Services, err = f.getServices(cluster.ID, cluster.Apps)
	if err != nil {
		return nil, err
	}

	// getIngressControllers should be called after getServices because it depends on service information.
	cluster.IngressControllers, err = f.getIngressControllers(cluster.Services, cluster.Apps)
	if err != nil {
		return nil, err
	}

	cluster.Ingresses, err = f.getIngresses(cluster.ID)
	if err != nil {
		return nil, err
	}

	cluster.IngressRoutes, err = f.getIngressRoutes(cluster.ID)
	if err != nil {
		return nil, err
	}

	cluster.AccessControlPolicies, err = f.getAccessControlPolicies(cluster.ID)
	if err != nil {
		return nil, err
	}

	cluster.Overview = getOverview(cluster)

	return cluster, nil
}

func hasTraefikCRDs(clientSet discovery.DiscoveryInterface) (bool, error) {
	crdList, err := clientSet.ServerResourcesForGroupVersion(traefikv1alpha1.SchemeGroupVersion.String())
	if err != nil {
		if kerror.IsNotFound(err) ||
			// because the fake client doesn't return the right error type.
			strings.HasSuffix(err.Error(), " not found") {
			return false, nil
		}
		return false, err
	}

	for _, kind := range []string{ResourceKindIngressRoute, ResourceKindTraefikService, ResourceKindTLSOption} {
		var exists bool
		for _, resource := range crdList.APIResources {
			if resource.Kind == kind {
				exists = true
				break
			}
		}

		if !exists {
			return false, nil
		}
	}

	return true, nil
}

func getOverview(state *Cluster) Overview {
	var ctrlTypes []string
	existingTypes := make(map[string]struct{})

	for _, ic := range state.IngressControllers {
		if _, exists := existingTypes[ic.Type]; exists {
			continue
		}

		ctrlTypes = append(ctrlTypes, ic.Type)
		existingTypes[ic.Type] = struct{}{}
	}

	sort.Strings(ctrlTypes)

	return Overview{
		IngressCount:           len(state.Ingresses) + len(state.IngressRoutes),
		ServiceCount:           len(state.Services),
		IngressControllerTypes: ctrlTypes,
	}
}

func objectKey(name, ns string) string {
	return name + "@" + ns
}

func ingressKey(meta ResourceMeta) string {
	return fmt.Sprintf("%s.%s.%s", objectKey(meta.Name, meta.Namespace), strings.ToLower(meta.Kind), meta.Group)
}

func sanitizeAnnotations(annotations map[string]string) map[string]string {
	if annotations == nil {
		return nil
	}

	result := make(map[string]string)
	for name, value := range annotations {
		if name == "kubectl.kubernetes.io/last-applied-configuration" {
			continue
		}

		result[name] = value
	}

	return result
}

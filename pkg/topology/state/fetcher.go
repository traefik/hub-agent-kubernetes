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
	"strings"
	"time"

	"github.com/hashicorp/go-version"
	"github.com/traefik/hub-agent-kubernetes/pkg/kube"
	"github.com/traefik/hub-agent-kubernetes/pkg/kubevers"
	"k8s.io/client-go/informers"
	clientset "k8s.io/client-go/kubernetes"
)

// Fetcher fetches Kubernetes resources and converts them into a filtered and simplified state.
type Fetcher struct {
	serverVersion string

	k8s       informers.SharedInformerFactory
	clientSet clientset.Interface
}

// NewFetcher creates a new Fetcher.
func NewFetcher(ctx context.Context) (*Fetcher, error) {
	config, err := kube.InClusterConfigWithRetrier(2)
	if err != nil {
		return nil, fmt.Errorf("create Kubernetes in-cluster configuration: %w", err)
	}

	clientSet, err := clientset.NewForConfig(config)
	if err != nil {
		return nil, err
	}

	serverVersion, err := clientSet.Discovery().ServerVersion()
	if err != nil {
		return nil, fmt.Errorf("get server version: %w", err)
	}

	return watchAll(ctx, clientSet, serverVersion.GitVersion)
}

func watchAll(ctx context.Context, clientSet clientset.Interface, serverVersion string) (*Fetcher, error) {
	serverSemVer, err := version.NewVersion(serverVersion)
	if err != nil {
		return nil, fmt.Errorf("parse server version: %w", err)
	}

	if serverSemVer.LessThan(version.Must(version.NewVersion("1.14"))) {
		return nil, fmt.Errorf("unsupported version: %s", serverSemVer)
	}

	kubernetesFactory := informers.NewSharedInformerFactoryWithOptions(clientSet, 5*time.Minute)

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

	kubernetesFactory.Start(ctx.Done())

	for typ, ok := range kubernetesFactory.WaitForCacheSync(ctx.Done()) {
		if !ok {
			return nil, fmt.Errorf("timed out waiting for k8s object caches to sync %s", typ)
		}
	}

	return &Fetcher{
		serverVersion: serverVersion,
		k8s:           kubernetesFactory,
		clientSet:     clientSet,
	}, nil
}

// FetchState assembles a cluster state from Kubernetes resources.
func (f *Fetcher) FetchState() (*Cluster, error) {
	var cluster Cluster

	var err error

	cluster.Services, err = f.getServices()
	if err != nil {
		return nil, err
	}

	cluster.Ingresses, err = f.getIngresses()
	if err != nil {
		return nil, err
	}

	cluster.Overview = Overview{
		ServiceCount: len(cluster.Services),
	}

	return &cluster, nil
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

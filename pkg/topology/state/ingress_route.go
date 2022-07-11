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
	"strings"

	traefikv1alpha1 "github.com/traefik/hub-agent-kubernetes/pkg/crd/api/traefik/v1alpha1"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/util/intstr"
)

// Supported Traefik CRD kinds.
const (
	ResourceKindIngressRoute   = "IngressRoute"
	ResourceKindTraefikService = "TraefikService"
	ResourceKindTLSOption      = "TLSOption"
)

func (f *Fetcher) getIngressRoutes(clusterID string) (map[string]*IngressRoute, error) {
	ingressRoutes, err := f.traefik.Traefik().V1alpha1().IngressRoutes().Lister().List(labels.Everything())
	if err != nil {
		return nil, err
	}

	result := make(map[string]*IngressRoute)
	for _, ingressRoute := range ingressRoutes {
		var routes []Route
		for _, route := range ingressRoute.Spec.Routes {
			services, err := f.getRouteServices(ingressRoute.Namespace, route)
			if err != nil {
				return nil, err
			}

			routes = append(routes, Route{
				Match:    route.Match,
				Services: services,
			})
		}

		var tls *IngressRouteTLS
		if ingressRoute.Spec.TLS != nil {
			tls = &IngressRouteTLS{
				Domains:    ingressRoute.Spec.TLS.Domains,
				SecretName: ingressRoute.Spec.TLS.SecretName,
			}
			if ingressRoute.Spec.TLS.Options != nil {
				tls.Options = &TLSOptionRef{
					Name:      ingressRoute.Spec.TLS.Options.Name,
					Namespace: ingressRoute.Spec.TLS.Options.Namespace,
				}
			}
		}

		ing := &IngressRoute{
			ResourceMeta: ResourceMeta{
				Kind:      ResourceKindIngressRoute,
				Group:     traefikv1alpha1.GroupName,
				Name:      ingressRoute.Name,
				Namespace: ingressRoute.Namespace,
			},
			IngressMeta: IngressMeta{
				ClusterID:      clusterID,
				ControllerType: IngressControllerTypeTraefik,
				Annotations:    sanitizeAnnotations(ingressRoute.Annotations),
			},
			TLS:      tls,
			Routes:   routes,
			Services: getIngressRouteServices(routes),
		}

		result[ingressKey(ing.ResourceMeta)] = ing
	}

	return result, nil
}

func (f *Fetcher) getRouteServices(ingressRouteNamespace string, route traefikv1alpha1.Route) ([]RouteService, error) {
	var result []RouteService
	for _, service := range route.Services {
		if service.Kind != ResourceKindTraefikService {
			result = append(result, toRouteService(ingressRouteNamespace, &service.LoadBalancerSpec))
			continue
		}

		services, err := f.getRouteServicesFromTraefikService(ingressRouteNamespace, service.Namespace, service.Name)
		if err != nil {
			return nil, err
		}

		result = append(result, services...)
	}

	return result, nil
}

func (f *Fetcher) getRouteServicesFromTraefikService(parentNamespace, namespace, name string) ([]RouteService, error) {
	// Here we have to ignore TraefikServices with the cross-provider syntax (containing an @ in the name) as they don't exist in Kubernetes.
	if strings.Contains(name, "@") {
		return nil, nil
	}

	if namespace == "" {
		namespace = parentNamespace
	}

	ts, err := f.traefik.Traefik().V1alpha1().TraefikServices().Lister().TraefikServices(namespace).Get(name)
	if err != nil {
		return nil, err
	}

	if ts.Spec.Mirroring != nil {
		if ts.Spec.Mirroring.Kind != ResourceKindTraefikService {
			return []RouteService{toRouteService(namespace, &ts.Spec.Mirroring.LoadBalancerSpec)}, nil
		}

		services, err := f.getRouteServicesFromTraefikService(namespace, ts.Spec.Mirroring.Namespace, ts.Spec.Mirroring.Name)
		if err != nil {
			return nil, err
		}

		// TODO should we handle mirror services?
		return services, nil
	}

	// TraefikService should be of type Mirror or Weighted.
	if ts.Spec.Weighted == nil {
		return nil, nil
	}

	var result []RouteService
	for _, service := range ts.Spec.Weighted.Services {
		if service.Kind != ResourceKindTraefikService {
			result = append(result, toRouteService(namespace, &service.LoadBalancerSpec))
			continue
		}

		services, err := f.getRouteServicesFromTraefikService(namespace, service.Namespace, service.Name)
		if err != nil {
			return nil, err
		}
		result = append(result, services...)
	}

	return result, nil
}

func toRouteService(parentNamespace string, service *traefikv1alpha1.LoadBalancerSpec) RouteService {
	result := RouteService{
		Namespace: service.Namespace,
		Name:      service.Name,
	}

	if service.Namespace == "" {
		result.Namespace = parentNamespace
	}

	switch service.Port.Type {
	case intstr.Int:
		result.PortNumber = service.Port.IntVal
	case intstr.String:
		result.PortName = service.Port.StrVal
	}

	return result
}

func getIngressRouteServices(routes []Route) []string {
	var result []string

	knownServices := make(map[string]struct{})

	for _, r := range routes {
		for _, s := range r.Services {
			key := objectKey(s.Name, s.Namespace)
			if _, exists := knownServices[key]; exists {
				continue
			}

			knownServices[key] = struct{}{}
			result = append(result, key)
		}
	}

	return result
}

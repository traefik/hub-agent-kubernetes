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
	corev1 "k8s.io/api/core/v1"
	netv1 "k8s.io/api/networking/v1"
)

// Cluster describes a Cluster.
type Cluster struct {
	Services  map[string]*Service `json:"services"`
	Ingresses map[string]*Ingress `json:"-"`
}

// Service describes a Service.
type Service struct {
	Name          string             `json:"name"`
	Namespace     string             `json:"namespace"`
	Type          corev1.ServiceType `json:"type"`
	Annotations   map[string]string  `json:"annotations,omitempty"`
	ExternalIPs   []string           `json:"externalIPs,omitempty"`
	ExternalPorts []int              `json:"externalPorts,omitempty"`
}

// ResourceMeta represents the metadata which identify a Kubernetes resource.
type ResourceMeta struct {
	Kind      string `json:"kind"`
	Group     string `json:"group"`
	Name      string `json:"name"`
	Namespace string `json:"namespace"`
}

// IngressMeta represents the common Ingress metadata properties.
type IngressMeta struct {
	ControllerType string            `json:"controllerType,omitempty"`
	Annotations    map[string]string `json:"annotations,omitempty"`
}

// Ingress describes an Kubernetes Ingress.
type Ingress struct {
	ResourceMeta
	IngressMeta

	IngressClassName *string               `json:"ingressClassName,omitempty"`
	TLS              []netv1.IngressTLS    `json:"tls,omitempty"`
	Rules            []netv1.IngressRule   `json:"rules,omitempty"`
	DefaultBackend   *netv1.IngressBackend `json:"defaultBackend,omitempty"`
	Services         []string              `json:"services,omitempty"`
}

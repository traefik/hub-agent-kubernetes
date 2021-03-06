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
	traefikv1alpha1 "github.com/traefik/hub-agent-kubernetes/pkg/crd/api/traefik/v1alpha1"
	corev1 "k8s.io/api/core/v1"
	netv1 "k8s.io/api/networking/v1"
)

// Cluster describes a Cluster.
type Cluster struct {
	ID                    string
	Overview              Overview
	Namespaces            []string
	Apps                  map[string]*App
	Ingresses             map[string]*Ingress
	IngressRoutes         map[string]*IngressRoute `dir:"Ingresses"`
	Services              map[string]*Service
	IngressControllers    map[string]*IngressController
	AccessControlPolicies map[string]*AccessControlPolicy
	TLSOptions            map[string]*TLSOptions

	TraefikServiceNames map[string]string `dir:"-"`
}

// Overview represents an overview of the cluster resources.
type Overview struct {
	IngressCount           int      `json:"ingressCount"`
	ServiceCount           int      `json:"serviceCount"`
	IngressControllerTypes []string `json:"ingressControllerTypes"`
}

// ResourceMeta represents the metadata which identify a Kubernetes resource.
type ResourceMeta struct {
	Kind      string `json:"kind"`
	Group     string `json:"group"`
	Name      string `json:"name"`
	Namespace string `json:"namespace"`
}

// App is an abstraction of Deployments/ReplicaSets/DaemonSets/StatefulSets.
type App struct {
	Name          string            `json:"name"`
	Kind          string            `json:"kind"`
	Namespace     string            `json:"namespace"`
	Replicas      int               `json:"replicas"`
	ReadyReplicas int               `json:"readyReplicas"`
	Images        []string          `json:"images,omitempty"`
	Labels        map[string]string `json:"labels,omitempty"`

	podLabels map[string]string
}

// IngressController is an abstraction of Deployments/ReplicaSets/DaemonSets/StatefulSets that
// are a cluster's IngressController.
type IngressController struct {
	App

	Type            string   `json:"type"`
	IngressClasses  []string `json:"ingressClasses,omitempty"`
	MetricsURLs     []string `json:"metricsURLs,omitempty"`
	PublicEndpoints []string `json:"publicEndpoints,omitempty"`
	Endpoints       []string `json:"endpoints,omitempty"`
}

// Service describes a Service.
type Service struct {
	Name          string             `json:"name"`
	Namespace     string             `json:"namespace"`
	ClusterID     string             `json:"clusterId"`
	Type          corev1.ServiceType `json:"type"`
	Selector      map[string]string  `json:"selector"`
	Apps          []string           `json:"apps,omitempty"`
	Annotations   map[string]string  `json:"annotations,omitempty"`
	ExternalIPs   []string           `json:"externalIPs,omitempty"`
	ExternalPorts []int              `json:"externalPorts,omitempty"`

	status corev1.ServiceStatus
}

// IngressMeta represents the common Ingress metadata properties.
type IngressMeta struct {
	ClusterID      string            `json:"clusterId"`
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

// IngressRoute describes a Traefik IngressRoute.
type IngressRoute struct {
	ResourceMeta
	IngressMeta

	TLS      *IngressRouteTLS `json:"tls,omitempty"`
	Routes   []Route          `json:"routes,omitempty"`
	Services []string         `json:"services,omitempty"`
}

// IngressRouteTLS represents a simplified Traefik IngressRoute TLS configuration.
type IngressRouteTLS struct {
	Domains    []traefikv1alpha1.Domain `json:"domains,omitempty"`
	SecretName string                   `json:"secretName,omitempty"`
	Options    *TLSOptionRef            `json:"options,omitempty"`
}

// TLSOptionRef references TLSOptions.
type TLSOptionRef struct {
	Name      string `json:"name"`
	Namespace string `json:"namespace,omitempty"`
}

// Route represents a Traefik IngressRoute route.
type Route struct {
	Match    string         `json:"match"`
	Services []RouteService `json:"services,omitempty"`
}

// RouteService represents a Kubernetes service targeted by a Traefik IngressRoute route.
type RouteService struct {
	Namespace  string `json:"namespace"`
	Name       string `json:"name"`
	PortName   string `json:"portName,omitempty"`
	PortNumber int32  `json:"portNumber,omitempty"`
}

// AccessControlPolicy describes an Access Control Policy configured within a cluster.
type AccessControlPolicy struct {
	Name      string                        `json:"name"`
	Namespace string                        `json:"namespace"`
	ClusterID string                        `json:"clusterId"`
	Method    string                        `json:"method"`
	JWT       *AccessControlPolicyJWT       `json:"jwt,omitempty"`
	BasicAuth *AccessControlPolicyBasicAuth `json:"basicAuth,omitempty"`
}

// AccessControlPolicyJWT describes the settings for JWT authentication within an access control policy.
type AccessControlPolicyJWT struct {
	SigningSecret              string            `json:"signingSecret,omitempty"`
	SigningSecretBase64Encoded bool              `json:"signingSecretBase64Encoded"`
	PublicKey                  string            `json:"publicKey,omitempty"`
	JWKsFile                   string            `json:"jwksFile,omitempty"`
	JWKsURL                    string            `json:"jwksUrl,omitempty"`
	StripAuthorizationHeader   bool              `json:"stripAuthorizationHeader,omitempty"`
	ForwardHeaders             map[string]string `json:"forwardHeaders,omitempty"`
	TokenQueryKey              string            `json:"tokenQueryKey,omitempty"`
	Claims                     string            `json:"claims,omitempty"`
}

// AccessControlPolicyBasicAuth holds the HTTP basic authentication configuration.
type AccessControlPolicyBasicAuth struct {
	Users                    string `json:"users,omitempty"`
	Realm                    string `json:"realm,omitempty"`
	StripAuthorizationHeader bool   `json:"stripAuthorizationHeader,omitempty"`
	ForwardUsernameHeader    string `json:"forwardUsernameHeader,omitempty"`
}

// TLSOptions holds TLS options.
type TLSOptions struct {
	Name                     string                     `json:"name"`
	Namespace                string                     `json:"namespace"`
	MinVersion               string                     `json:"minVersion,omitempty"`
	MaxVersion               string                     `json:"maxVersion,omitempty"`
	CipherSuites             []string                   `json:"cipherSuites,omitempty"`
	CurvePreferences         []string                   `json:"curvePreferences,omitempty"`
	ClientAuth               traefikv1alpha1.ClientAuth `json:"clientAuth"`
	SniStrict                bool                       `json:"sniStrict"`
	PreferServerCipherSuites bool                       `json:"preferServerCipherSuites"`
}

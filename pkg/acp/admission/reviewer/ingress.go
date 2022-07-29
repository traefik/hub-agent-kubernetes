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
	"encoding/json"
	"errors"
	"fmt"

	"github.com/traefik/hub-agent-kubernetes/pkg/acp"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// AnnotationHubAuth is the annotation to add to an Ingress resource in order to enable Hub authentication.
const AnnotationHubAuth = "hub.traefik.io/access-control-policy"

// Ingress controller default annotations.
const (
	defaultAnnotationTraefik = "traefik"
)

// ingress is a generic form of netv1, netv1beta1 and extv1 ingress resources.
type ingress struct {
	Metadata metav1.ObjectMeta `json:"metadata"`
	Spec     ingressSpec       `json:"spec"`
}

type ingressSpec struct {
	Rules []ingressRule `json:"rules"`
}

type ingressRule struct {
	Host string           `json:"host"`
	HTTP *ingressRuleHTTP `json:"http"`
}

type ingressRuleHTTP struct {
	Paths []interface{} `json:"paths"`
}

// parseRawIngresses parses raw objects from admission requests into generic ingress resources.
func parseRawIngresses(newRaw, oldRaw []byte) (newIng, oldIng ingress, err error) {
	if err = json.Unmarshal(newRaw, &newIng); err != nil {
		return ingress{}, ingress{}, fmt.Errorf("unmarshal reviewed ingress: %w", err)
	}

	if oldRaw != nil {
		if err = json.Unmarshal(oldRaw, &oldIng); err != nil {
			return ingress{}, ingress{}, fmt.Errorf("unmarshal reviewed old ingress: %w", err)
		}
	}

	return newIng, oldIng, nil
}

// parseIngressClass parses a raw, JSON-marshaled ingress and returns the ingress class it refers to.
func parseIngressClass(obj []byte) (ingClassName, ingClassAnno string, err error) {
	var ing struct {
		ObjectMeta struct {
			Annotations map[string]string `json:"annotations"`
		} `json:"metadata"`
		Spec struct {
			IngressClassName string `json:"ingressClassName"`
		} `json:"spec"`
	}
	if err = json.Unmarshal(obj, &ing); err != nil {
		return "", "", err
	}

	return ing.Spec.IngressClassName, ing.ObjectMeta.Annotations["kubernetes.io/ingress.class"], nil
}

func headerToForward(cfg *acp.Config) ([]string, error) {
	var headerToFwd []string
	switch {
	case cfg.JWT != nil:
		for headerName := range cfg.JWT.ForwardHeaders {
			headerToFwd = append(headerToFwd, headerName)
		}
		if cfg.JWT.StripAuthorizationHeader {
			headerToFwd = append(headerToFwd, "Authorization")
		}
	case cfg.BasicAuth != nil:
		if headerName := cfg.BasicAuth.ForwardUsernameHeader; headerName != "" {
			headerToFwd = append(headerToFwd, headerName)
		}
		if cfg.BasicAuth.StripAuthorizationHeader {
			headerToFwd = append(headerToFwd, "Authorization")
		}
	case cfg.OIDC != nil:
		for headerName := range cfg.OIDC.ForwardHeaders {
			headerToFwd = append(headerToFwd, headerName)
		}
		headerToFwd = append(headerToFwd, "Set-Cookie")

	default:
		return nil, errors.New("unsupported ACP type")
	}
	return headerToFwd, nil
}

func isDefaultIngressClassValue(value string) bool {
	switch value {
	case defaultAnnotationTraefik:
		return true
	default:
		return false
	}
}

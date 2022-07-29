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

package v1alpha1

import (
	"crypto/sha1"
	"encoding/base64"
	"encoding/json"
	"fmt"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// +genclient
// +genclient:nonNamespaced
// +k8s:deepcopy-gen:interfaces=k8s.io/apimachinery/pkg/runtime.Object

// AccessControlPolicy defines an access control policy.
// +kubebuilder:resource:scope=Cluster
type AccessControlPolicy struct {
	metav1.TypeMeta `json:",inline"`
	// +optional
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec AccessControlPolicySpec `json:"spec,omitempty"`

	// The current status of this access control policy.
	// +optional
	Status AccessControlPolicyStatus `json:"status,omitempty"`
}

// AccessControlPolicySpec configures an access control policy.
type AccessControlPolicySpec struct {
	JWT       *AccessControlPolicyJWT       `json:"jwt,omitempty"`
	BasicAuth *AccessControlPolicyBasicAuth `json:"basicAuth,omitempty"`
	OIDC      *AccessControlOIDC            `json:"oidc,omitempty"`
}

// Hash return AccessControlPolicySpec hash.
func (a AccessControlPolicySpec) Hash() (string, error) {
	b, err := json.Marshal(a)
	if err != nil {
		return "", fmt.Errorf("encode ACP spec: %w", err)
	}

	hash := sha1.New()
	hash.Write(b)

	return base64.StdEncoding.EncodeToString(hash.Sum(nil)), nil
}

// AccessControlPolicyJWT configures a JWT access control policy.
type AccessControlPolicyJWT struct {
	SigningSecret              string            `json:"signingSecret,omitempty"`
	SigningSecretBase64Encoded bool              `json:"signingSecretBase64Encoded,omitempty"`
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
	Users                    []string `json:"users,omitempty"`
	Realm                    string   `json:"realm,omitempty"`
	StripAuthorizationHeader bool     `json:"stripAuthorizationHeader,omitempty"`
	ForwardUsernameHeader    string   `json:"forwardUsernameHeader,omitempty"`
}

// AccessControlOIDC holds the OIDC authentication configuration.
type AccessControlOIDC struct {
	Issuer       string `json:"issuer,omitempty"`
	ClientID     string `json:"clientId,omitempty"`
	ClientSecret string `json:"clientSecret,omitempty"`
	TLS          *TLS   `json:"tls,omitempty"`

	RedirectURL string            `json:"redirectUrl,omitempty"`
	LogoutURL   string            `json:"logoutUrl,omitempty"`
	AuthParams  map[string]string `json:"authParams,omitempty"`

	StateCookie StateCookie `json:"stateCookie"`
	Session     Session     `json:"session"`

	Scopes         []string          `json:"scopes,omitempty"`
	ForwardHeaders map[string]string `json:"forwardHeaders,omitempty"`
	Claims         string            `json:"claims,omitempty"`
}

// TLS holds the TLS configuration.
type TLS struct {
	CABundle           []byte `json:"caBundle"`
	InsecureSkipVerify bool   `json:"insecureSkipVerify"`
}

// StateCookie holds state cookie configuration.
type StateCookie struct {
	Secret   string `json:"secret"`
	SameSite string `json:"sameSite,omitempty"`
	Secure   bool   `json:"secure,omitempty"`
	Domain   string `json:"domain,omitempty"`
	Path     string `json:"path,omitempty"`
}

// Session holds session configuration.
type Session struct {
	Secret   string `json:"secret"`
	SameSite string `json:"sameSite,omitempty"`
	Secure   bool   `json:"secure,omitempty"`
	Domain   string `json:"domain,omitempty"`
	Path     string `json:"path,omitempty"`
	Refresh  *bool  `json:"refresh,omitempty"`
}

// AccessControlPolicyStatus is the status of the access control policy.
type AccessControlPolicyStatus struct {
	Version  string      `json:"version,omitempty"`
	SyncedAt metav1.Time `json:"syncedAt,omitempty"`
	SpecHash string      `json:"specHash,omitempty"`
}

// +k8s:deepcopy-gen:interfaces=k8s.io/apimachinery/pkg/runtime.Object

// AccessControlPolicyList defines a list of access control policy.
type AccessControlPolicyList struct {
	metav1.TypeMeta `json:",inline"`
	// +optional
	metav1.ListMeta `son:"metadata,omitempty"`

	Items []AccessControlPolicy `json:"items"`
}

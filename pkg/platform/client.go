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

package platform

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"path"
	"time"

	"github.com/hashicorp/go-cleanhttp"
	"github.com/hashicorp/go-retryablehttp"
	"github.com/rs/zerolog/log"
	"github.com/traefik/hub-agent-kubernetes/pkg/acp"
	hubv1alpha1 "github.com/traefik/hub-agent-kubernetes/pkg/crd/api/hub/v1alpha1"
	"github.com/traefik/hub-agent-kubernetes/pkg/edgeingress"
	"github.com/traefik/hub-agent-kubernetes/pkg/logger"
	"github.com/traefik/hub-agent-kubernetes/pkg/topology/state"
)

// APIError represents an error returned by the API.
type APIError struct {
	StatusCode int
	Retryable  bool
	Message    string `json:"error"`
}

func (a APIError) Error() string {
	return fmt.Sprintf("failed with code %d: %s", a.StatusCode, a.Message)
}

// Client allows interacting with the cluster service.
type Client struct {
	baseURL             *url.URL
	token               string
	retryableHTTPClient *http.Client
	httpClient          *http.Client
}

// NewClient creates a new client for the cluster service.
func NewClient(baseURL, token string) (*Client, error) {
	u, err := url.ParseRequestURI(baseURL)
	if err != nil {
		return nil, fmt.Errorf("parse client url: %w", err)
	}

	client := cleanhttp.DefaultPooledClient()
	client.Timeout = 10 * time.Second

	retryableClient := retryablehttp.NewClient()
	retryableClient.HTTPClient = client
	retryableClient.RetryMax = 4
	retryableClient.Logger = logger.NewWrappedLogger(log.Logger.With().Str("component", "platform_client").Logger())

	return &Client{
		baseURL:             u,
		token:               token,
		retryableHTTPClient: retryableClient.StandardClient(),
		httpClient:          client,
	}, nil
}

type linkClusterReq struct {
	KubeID   string `json:"kubeId"`
	Platform string `json:"platform"`
}

type linkClusterResp struct {
	ClusterID string `json:"clusterId"`
}

// Link links the agent to the given Kubernetes ID.
func (c *Client) Link(ctx context.Context, kubeID string) (string, error) {
	body, err := json.Marshal(linkClusterReq{KubeID: kubeID, Platform: "kubernetes"})
	if err != nil {
		return "", fmt.Errorf("marshal link agent request: %w", err)
	}

	baseURL, err := c.baseURL.Parse(path.Join(c.baseURL.Path, "link"))
	if err != nil {
		return "", fmt.Errorf("parse endpoint: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, baseURL.String(), bytes.NewReader(body))
	if err != nil {
		return "", fmt.Errorf("build request: %w", err)
	}

	req.Header.Set("Authorization", "Bearer "+c.token)

	resp, err := c.retryableHTTPClient.Do(req)
	if err != nil {
		return "", err
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK {
		if resp.StatusCode == http.StatusConflict {
			return "", errors.New("this token is already used by an agent in another Kubernetes cluster")
		}

		apiErr := APIError{StatusCode: resp.StatusCode}
		if err = json.NewDecoder(resp.Body).Decode(&apiErr); err != nil {
			return "", fmt.Errorf("failed with code %d: decode response: %w", resp.StatusCode, err)
		}

		return "", apiErr
	}

	var linkResp linkClusterResp
	if err = json.NewDecoder(resp.Body).Decode(&linkResp); err != nil {
		return "", fmt.Errorf("decode link agent resp: %w", err)
	}

	return linkResp.ClusterID, nil
}

// Config holds the configuration of the offer.
type Config struct {
	Metrics MetricsConfig `json:"metrics"`
}

// MetricsConfig holds the metrics part of the offer config.
type MetricsConfig struct {
	Interval time.Duration `json:"interval"`
	Tables   []string      `json:"tables"`
}

// GetConfig returns the agent configuration.
func (c *Client) GetConfig(ctx context.Context) (Config, error) {
	baseURL, err := c.baseURL.Parse(path.Join(c.baseURL.Path, "config"))
	if err != nil {
		return Config{}, fmt.Errorf("parse endpoint: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, baseURL.String(), http.NoBody)
	if err != nil {
		return Config{}, fmt.Errorf("build request: %w", err)
	}

	req.Header.Set("Authorization", "Bearer "+c.token)

	resp, err := c.retryableHTTPClient.Do(req)
	if err != nil {
		return Config{}, err
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK {
		all, _ := io.ReadAll(resp.Body)

		apiErr := APIError{StatusCode: resp.StatusCode}
		if err = json.Unmarshal(all, &apiErr); err != nil {
			apiErr.Message = string(all)
		}

		return Config{}, apiErr
	}

	var cfg Config
	if err = json.NewDecoder(resp.Body).Decode(&cfg); err != nil {
		return Config{}, fmt.Errorf("decode config: %w", err)
	}

	return cfg, nil
}

// GetACPs returns the ACPs related to the agent.
func (c *Client) GetACPs(ctx context.Context) ([]acp.ACP, error) {
	baseURL, err := c.baseURL.Parse(path.Join(c.baseURL.Path, "acps"))
	if err != nil {
		return nil, fmt.Errorf("parse endpoint: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, baseURL.String(), http.NoBody)
	if err != nil {
		return nil, fmt.Errorf("build request: %w", err)
	}

	req.Header.Set("Authorization", "Bearer "+c.token)

	resp, err := c.retryableHTTPClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK {
		all, _ := io.ReadAll(resp.Body)

		apiErr := APIError{StatusCode: resp.StatusCode}
		if err = json.Unmarshal(all, &apiErr); err != nil {
			apiErr.Message = string(all)
		}

		return nil, apiErr
	}

	var acps []acp.ACP
	if err = json.NewDecoder(resp.Body).Decode(&acps); err != nil {
		return nil, fmt.Errorf("decode config: %w", err)
	}

	return acps, nil
}

// Ping sends a ping to the platform to inform that the agent is alive.
func (c *Client) Ping(ctx context.Context) error {
	baseURL, err := c.baseURL.Parse(path.Join(c.baseURL.Path, "ping"))
	if err != nil {
		return fmt.Errorf("parse endpoint: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, baseURL.String(), http.NoBody)
	if err != nil {
		return fmt.Errorf("build request: %w", err)
	}

	req.Header.Set("Authorization", "Bearer "+c.token)

	resp, err := c.retryableHTTPClient.Do(req)
	if err != nil {
		return err
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("failed with code %d", resp.StatusCode)
	}
	return nil
}

// ListVerifiedDomains list verified domains.
func (c *Client) ListVerifiedDomains(ctx context.Context) ([]string, error) {
	baseURL, err := c.baseURL.Parse(path.Join(c.baseURL.Path, "verified-domains"))
	if err != nil {
		return nil, fmt.Errorf("parse endpoint: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, baseURL.String(), http.NoBody)
	if err != nil {
		return nil, fmt.Errorf("build request for %q: %w", baseURL.String(), err)
	}

	req.Header.Set("Authorization", "Bearer "+c.token)

	resp, err := c.retryableHTTPClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("request %q: %w", baseURL.String(), err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK {
		all, _ := io.ReadAll(resp.Body)

		apiErr := APIError{StatusCode: resp.StatusCode}
		if err = json.Unmarshal(all, &apiErr); err != nil {
			apiErr.Message = string(all)
		}

		return nil, apiErr
	}

	var domains []string
	if err = json.NewDecoder(resp.Body).Decode(&domains); err != nil {
		return nil, fmt.Errorf("failed to decode verified domains: %w", err)
	}

	return domains, nil
}

// CreateEdgeIngressReq is the request for creating an edge ingress.
type CreateEdgeIngressReq struct {
	Name      string  `json:"name"`
	Namespace string  `json:"namespace"`
	Service   Service `json:"service"`
	ACP       *ACP    `json:"acp,omitempty"`
}

// Service defines the service being exposed by the edge ingress.
type Service struct {
	Name string `json:"name"`
	Port int    `json:"port"`
}

// ACP defines the ACP attached to the edge ingress.
type ACP struct {
	Name string `json:"name"`
}

// ErrVersionConflict indicates a conflict error on the EdgeIngress resource being modified.
var ErrVersionConflict = errors.New("version conflict")

// CreateEdgeIngress creates an edge ingress.
func (c *Client) CreateEdgeIngress(ctx context.Context, createReq *CreateEdgeIngressReq) (*edgeingress.EdgeIngress, error) {
	body, err := json.Marshal(createReq)
	if err != nil {
		return nil, fmt.Errorf("marshal edge ingress request: %w", err)
	}

	baseURL, err := c.baseURL.Parse(path.Join(c.baseURL.Path, "edge-ingresses"))
	if err != nil {
		return nil, fmt.Errorf("parse endpoint: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, baseURL.String(), bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("build request for %q: %w", baseURL.String(), err)
	}

	req.Header.Set("Authorization", "Bearer "+c.token)

	resp, err := c.retryableHTTPClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("request %q: %w", baseURL.String(), err)
	}
	defer func() { _ = resp.Body.Close() }()

	switch resp.StatusCode {
	case http.StatusConflict:
		return nil, ErrVersionConflict
	case http.StatusCreated:
		var edgeIng edgeingress.EdgeIngress

		if err = json.NewDecoder(resp.Body).Decode(&edgeIng); err != nil {
			return nil, fmt.Errorf("failed to decode edge ingress: %w", err)
		}
		return &edgeIng, nil
	default:
		all, _ := io.ReadAll(resp.Body)

		apiErr := APIError{StatusCode: resp.StatusCode}
		if err = json.Unmarshal(all, &apiErr); err != nil {
			apiErr.Message = string(all)
		}

		return nil, apiErr
	}
}

// UpdateEdgeIngressReq is a request for updating an edge ingress.
type UpdateEdgeIngressReq struct {
	Service Service `json:"service"`
	ACP     *ACP    `json:"acp,omitempty"`
}

// UpdateEdgeIngress updated an edge ingress.
func (c *Client) UpdateEdgeIngress(ctx context.Context, namespace, name, lastKnownVersion string, updateReq *UpdateEdgeIngressReq) (*edgeingress.EdgeIngress, error) {
	body, err := json.Marshal(updateReq)
	if err != nil {
		return nil, fmt.Errorf("marshal edge ingress request: %w", err)
	}

	id := name + "@" + namespace
	baseURL, err := c.baseURL.Parse(path.Join(c.baseURL.Path, "edge-ingresses", id))
	if err != nil {
		return nil, fmt.Errorf("parse endpoint: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPut, baseURL.String(), bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("build request for %q: %w", baseURL.String(), err)
	}

	req.Header.Set("Authorization", "Bearer "+c.token)
	req.Header.Set("Last-Known-Version", lastKnownVersion)

	resp, err := c.retryableHTTPClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("request %q: %w", baseURL.String(), err)
	}
	defer func() { _ = resp.Body.Close() }()

	switch resp.StatusCode {
	case http.StatusConflict:
		return nil, ErrVersionConflict
	case http.StatusOK:
		var edgeIng edgeingress.EdgeIngress

		if err = json.NewDecoder(resp.Body).Decode(&edgeIng); err != nil {
			return nil, fmt.Errorf("failed to decode edge ingress: %w", err)
		}
		return &edgeIng, nil
	default:
		all, _ := io.ReadAll(resp.Body)

		apiErr := APIError{StatusCode: resp.StatusCode}
		if err = json.Unmarshal(all, &apiErr); err != nil {
			apiErr.Message = string(all)
		}

		return nil, apiErr
	}
}

// DeleteEdgeIngress deletes an edge ingress.
func (c *Client) DeleteEdgeIngress(ctx context.Context, namespace, name, lastKnownVersion string) error {
	id := name + "@" + namespace

	baseURL, err := c.baseURL.Parse(path.Join(c.baseURL.Path, "edge-ingresses", id))
	if err != nil {
		return fmt.Errorf("parse endpoint: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodDelete, baseURL.String(), http.NoBody)
	if err != nil {
		return fmt.Errorf("build request for %q: %w", baseURL.String(), err)
	}

	req.Header.Set("Authorization", "Bearer "+c.token)
	req.Header.Set("Last-Known-Version", lastKnownVersion)

	resp, err := c.retryableHTTPClient.Do(req)
	if err != nil {
		return fmt.Errorf("request %q: %w", baseURL.String(), err)
	}
	defer func() { _ = resp.Body.Close() }()

	switch resp.StatusCode {
	case http.StatusConflict:
		return ErrVersionConflict
	case http.StatusNoContent:
		return nil
	default:
		all, _ := io.ReadAll(resp.Body)

		apiErr := APIError{StatusCode: resp.StatusCode}
		if err = json.Unmarshal(all, &apiErr); err != nil {
			apiErr.Message = string(all)
		}

		return apiErr
	}
}

// CreateACP creates an AccessControlPolicy.
func (c *Client) CreateACP(ctx context.Context, policy *hubv1alpha1.AccessControlPolicy) (*acp.ACP, error) {
	acpReq := acp.ACP{
		Name:   policy.Name,
		Config: *acp.ConfigFromPolicy(policy),
	}
	body, err := json.Marshal(acpReq)
	if err != nil {
		return nil, fmt.Errorf("marshal ACP request: %w", err)
	}

	baseURL, err := c.baseURL.Parse(path.Join(c.baseURL.Path, "acps"))
	if err != nil {
		return nil, fmt.Errorf("parse endpoint: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, baseURL.String(), bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("build request for %q: %w", baseURL.String(), err)
	}

	req.Header.Set("Authorization", "Bearer "+c.token)

	resp, err := c.retryableHTTPClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("request %q: %w", baseURL.String(), err)
	}
	defer func() { _ = resp.Body.Close() }()

	switch resp.StatusCode {
	case http.StatusConflict:
		return nil, ErrVersionConflict
	case http.StatusCreated:
		var a acp.ACP
		if err = json.NewDecoder(resp.Body).Decode(&a); err != nil {
			return nil, fmt.Errorf("failed to decode ACP: %w", err)
		}

		return &a, nil
	default:
		all, _ := io.ReadAll(resp.Body)

		apiErr := APIError{StatusCode: resp.StatusCode}
		if err = json.Unmarshal(all, &apiErr); err != nil {
			apiErr.Message = string(all)
		}

		return nil, apiErr
	}
}

// UpdateACP updates an AccessControlPolicy.
func (c *Client) UpdateACP(ctx context.Context, oldVersion string, policy *hubv1alpha1.AccessControlPolicy) (*acp.ACP, error) {
	acpReq := acp.ACP{
		Name:   policy.Name,
		Config: *acp.ConfigFromPolicy(policy),
	}
	body, err := json.Marshal(acpReq)
	if err != nil {
		return nil, fmt.Errorf("marshal ACP request: %w", err)
	}

	baseURL, err := c.baseURL.Parse(path.Join(c.baseURL.Path, "acps", policy.Name))
	if err != nil {
		return nil, fmt.Errorf("parse endpoint: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPut, baseURL.String(), bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("build request for %q: %w", baseURL.String(), err)
	}

	req.Header.Set("Authorization", "Bearer "+c.token)
	req.Header.Set("Last-Known-Version", oldVersion)

	resp, err := c.retryableHTTPClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("request %q: %w", baseURL.String(), err)
	}
	defer func() { _ = resp.Body.Close() }()

	switch resp.StatusCode {
	case http.StatusConflict:
		return nil, ErrVersionConflict
	case http.StatusOK:
		var a acp.ACP
		if err = json.NewDecoder(resp.Body).Decode(&a); err != nil {
			return nil, fmt.Errorf("failed to decode ACP: %w", err)
		}

		return &a, nil
	default:
		all, _ := io.ReadAll(resp.Body)

		apiErr := APIError{StatusCode: resp.StatusCode}
		if err = json.Unmarshal(all, &apiErr); err != nil {
			apiErr.Message = string(all)
		}

		return nil, apiErr
	}
}

// DeleteACP deletes an AccessControlPolicy.
func (c *Client) DeleteACP(ctx context.Context, oldVersion, name string) error {
	baseURL, err := c.baseURL.Parse(path.Join(c.baseURL.Path, "acps", name))
	if err != nil {
		return fmt.Errorf("parse endpoint: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodDelete, baseURL.String(), http.NoBody)
	if err != nil {
		return fmt.Errorf("build request for %q: %w", baseURL.String(), err)
	}

	req.Header.Set("Authorization", "Bearer "+c.token)
	req.Header.Set("Last-Known-Version", oldVersion)

	resp, err := c.retryableHTTPClient.Do(req)
	if err != nil {
		return fmt.Errorf("request %q: %w", baseURL.String(), err)
	}
	defer func() { _ = resp.Body.Close() }()

	switch resp.StatusCode {
	case http.StatusConflict:
		return ErrVersionConflict
	case http.StatusNoContent:
		return nil
	default:
		all, _ := io.ReadAll(resp.Body)

		apiErr := APIError{StatusCode: resp.StatusCode}
		if err = json.Unmarshal(all, &apiErr); err != nil {
			apiErr.Message = string(all)
		}

		return apiErr
	}
}

// GetEdgeIngresses returns the EdgeIngresses related to the agent.
func (c *Client) GetEdgeIngresses(ctx context.Context) ([]edgeingress.EdgeIngress, error) {
	baseURL, err := c.baseURL.Parse(path.Join(c.baseURL.Path, "edge-ingresses"))
	if err != nil {
		return nil, fmt.Errorf("parse endpoint: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, baseURL.String(), http.NoBody)
	if err != nil {
		return nil, fmt.Errorf("build request: %w", err)
	}

	req.Header.Set("Authorization", "Bearer "+c.token)

	resp, err := c.retryableHTTPClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK {
		all, _ := io.ReadAll(resp.Body)

		apiErr := APIError{StatusCode: resp.StatusCode}
		if err = json.Unmarshal(all, &apiErr); err != nil {
			apiErr.Message = string(all)
		}

		return nil, apiErr
	}

	var edgeIngresses []edgeingress.EdgeIngress
	if err = json.NewDecoder(resp.Body).Decode(&edgeIngresses); err != nil {
		return nil, fmt.Errorf("decode config: %w", err)
	}

	return edgeIngresses, nil
}

// GetCertificate obtains a certificate for the workspace.
func (c *Client) GetCertificate(ctx context.Context) (edgeingress.Certificate, error) {
	baseURL, err := c.baseURL.Parse(path.Join(c.baseURL.Path, "wildcard-certificate"))
	if err != nil {
		return edgeingress.Certificate{}, fmt.Errorf("parse endpoint: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, baseURL.String(), http.NoBody)
	if err != nil {
		return edgeingress.Certificate{}, fmt.Errorf("build request: %w", err)
	}

	req.Header.Set("Authorization", "Bearer "+c.token)

	resp, err := c.retryableHTTPClient.Do(req)
	if err != nil {
		return edgeingress.Certificate{}, err
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK {
		all, _ := io.ReadAll(resp.Body)

		apiErr := APIError{StatusCode: resp.StatusCode}
		if err = json.Unmarshal(all, &apiErr); err != nil {
			apiErr.Message = string(all)
		}

		return edgeingress.Certificate{}, apiErr
	}

	var cert edgeingress.Certificate
	if err = json.NewDecoder(resp.Body).Decode(&cert); err != nil {
		return edgeingress.Certificate{}, fmt.Errorf("decode obtain resp: %w", err)
	}

	return cert, nil
}

type fetchResp struct {
	Version  string        `json:"version"`
	Topology state.Cluster `json:"topology"`
}

// FetchTopology fetches the topology.
func (c *Client) FetchTopology(ctx context.Context) (topology state.Cluster, version string, err error) {
	baseURL, err := c.baseURL.Parse(path.Join(c.baseURL.Path, "topology"))
	if err != nil {
		return state.Cluster{}, "", fmt.Errorf("parse endpoint: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, baseURL.String(), http.NoBody)
	if err != nil {
		return state.Cluster{}, "", fmt.Errorf("build request: %w", err)
	}

	req.Header.Set("Authorization", "Bearer "+c.token)

	resp, err := c.retryableHTTPClient.Do(req)
	if err != nil {
		return state.Cluster{}, "", err
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK {
		all, _ := io.ReadAll(resp.Body)

		apiErr := APIError{StatusCode: resp.StatusCode}
		if err = json.Unmarshal(all, &apiErr); err != nil {
			apiErr.Message = string(all)
		}

		return state.Cluster{}, "", apiErr
	}

	var body fetchResp
	if err = json.NewDecoder(resp.Body).Decode(&body); err != nil {
		return state.Cluster{}, "", fmt.Errorf("decode topology: %w", err)
	}

	return body.Topology, body.Version, nil
}

type patchResp struct {
	Version string `json:"version"`
}

// PatchTopology submits a JSON Merge Patch to the platform containing the difference in the topology since
// its last synchronization. The last known topology version must be provided. This version can be obtained
// by calling the FetchTopology method.
func (c *Client) PatchTopology(ctx context.Context, patch []byte, lastKnownVersion string) (string, error) {
	baseURL, err := c.baseURL.Parse(path.Join(c.baseURL.Path, "topology"))
	if err != nil {
		return "", fmt.Errorf("parse endpoint: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPatch, baseURL.String(), bytes.NewReader(patch))
	if err != nil {
		return "", fmt.Errorf("build request: %w", err)
	}

	req.Header.Set("Authorization", "Bearer "+c.token)
	req.Header.Set("Content-Type", "application/merge-patch+json")
	req.Header.Set("Last-Known-Version", lastKnownVersion)

	// This operation cannot be retried without calling FetchTopology in between.
	resp, err := c.httpClient.Do(req)
	if err != nil {
		return "", err
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK {
		all, _ := io.ReadAll(resp.Body)

		retryable := true
		if resp.StatusCode == http.StatusUnprocessableEntity || resp.StatusCode == http.StatusBadRequest {
			retryable = false
		}

		apiErr := APIError{StatusCode: resp.StatusCode, Retryable: retryable}
		if err = json.Unmarshal(all, &apiErr); err != nil {
			apiErr.Message = string(all)
		}

		return "", apiErr
	}

	var body patchResp
	if err = json.NewDecoder(resp.Body).Decode(&body); err != nil {
		return "", fmt.Errorf("decode topology: %w", err)
	}

	return body.Version, nil
}

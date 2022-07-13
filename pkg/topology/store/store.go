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

package store

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/cenkalti/backoff/v4"
	jsonpatch "github.com/evanphx/json-patch"
	"github.com/rs/zerolog/log"
	"github.com/traefik/hub-agent-kubernetes/pkg/platform"
	"github.com/traefik/hub-agent-kubernetes/pkg/topology/state"
)

// PlatformClient is capable of interacting with the platform.
type PlatformClient interface {
	FetchTopology(ctx context.Context) (topology state.Cluster, version string, err error)
	PatchTopology(ctx context.Context, patch []byte, lastKnownVersion string) (string, error)
}

// Store stores the topology on the platform.
type Store struct {
	platform PlatformClient

	lastTopology     []byte
	lastKnownVersion string
	backoff          backoff.BackOff
}

// New instantiates a new Store.
func New(platformClient PlatformClient) *Store {
	b := backoff.NewExponentialBackOff()
	b.InitialInterval = time.Second
	b.MaxElapsedTime = time.Minute

	return &Store{
		platform: platformClient,
		backoff:  b,
	}
}

// Write writes the topology on the platform.
func (s *Store) Write(ctx context.Context, st state.Cluster) error {
	return backoff.RetryNotify(func() error {
		if s.lastKnownVersion == "" {
			topology, version, err := s.platform.FetchTopology(ctx)
			if err != nil {
				return backoff.Permanent(fmt.Errorf("fetch topology: %w", err))
			}

			s.lastTopology, err = json.Marshal(topology)
			if err != nil {
				return backoff.Permanent(fmt.Errorf("marshal topology: %w", err))
			}

			s.lastKnownVersion = version
		}

		patch, newTopology, err := s.buildPatch(s.lastTopology, st)
		if err != nil {
			return backoff.Permanent(fmt.Errorf("build topology patch: %w", err))
		}
		if patch == nil {
			return nil
		}

		s.lastKnownVersion, err = s.platform.PatchTopology(ctx, patch, s.lastKnownVersion)
		if err == nil {
			s.lastTopology = newTopology
			return nil
		}

		var apiErr platform.APIError
		if !errors.As(err, &apiErr) || !apiErr.Retryable {
			return backoff.Permanent(fmt.Errorf("patch topology: %w", err))
		}

		return err
	}, s.backoff, func(err error, retryIn time.Duration) {
		log.Ctx(ctx).Warn().Err(err).Dur("retry_in", retryIn).Msg("Unable to patch topology")
	})
}

func (s *Store) buildPatch(lastTopology []byte, st state.Cluster) ([]byte, []byte, error) {
	newTopology, err := json.Marshal(st)
	if err != nil {
		return nil, nil, fmt.Errorf("marshal topology: %w", err)
	}

	if bytes.Equal(newTopology, lastTopology) {
		return nil, newTopology, nil
	}

	patch, err := jsonpatch.CreateMergePatch(lastTopology, newTopology)
	if err != nil {
		return nil, nil, fmt.Errorf("build merge patch: %w", err)
	}
	return patch, newTopology, nil
}

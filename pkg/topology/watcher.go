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

package topology

import (
	"context"
	"sync"
	"time"

	"github.com/rs/zerolog/log"
	"github.com/traefik/hub-agent-kubernetes/pkg/topology/state"
	"github.com/traefik/hub-agent-kubernetes/pkg/topology/store"
)

// ListenerFunc is a function called by the watcher with the
// current state.
type ListenerFunc func(ctx context.Context, state *state.Cluster)

// Watcher is a process from the Hub agent that watches the topology for changes and
// stores them over time to make them accessible from the SaaS.
type Watcher struct {
	k8s   *state.Fetcher
	store *store.Store

	listenersMu sync.Mutex
	listeners   []ListenerFunc
}

// NewWatcher instantiates a new watcher that uses a fetcher to periodically get the K8S state and a store to write it.
func NewWatcher(f *state.Fetcher, s *store.Store) *Watcher {
	return &Watcher{
		k8s:   f,
		store: s,
	}
}

// AddListener adds a state listener.
func (w *Watcher) AddListener(listener ListenerFunc) {
	w.listenersMu.Lock()
	defer w.listenersMu.Unlock()

	w.listeners = append(w.listeners, listener)
}

// Start runs the watcher process.
func (w *Watcher) Start(ctx context.Context) {
	tick := time.NewTicker(5 * time.Second)
	defer tick.Stop()

	for {
		select {
		case <-ctx.Done():
			log.Info().Msg("Stopping topology watcher")
			return
		case <-tick.C:
			s, err := w.k8s.FetchState()
			if err != nil {
				log.Error().Err(err).Msg("create state")
				continue
			}
			if s == nil {
				continue
			}

			w.listenersMu.Lock()
			for _, l := range w.listeners {
				l(ctx, s)
			}
			w.listenersMu.Unlock()

			if err = w.store.Write(ctx, *s); err != nil {
				log.Error().Err(err).Msg("commit cluster state changes")
			}
		}
	}
}

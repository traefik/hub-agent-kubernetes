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

package admission

import (
	"fmt"
	"reflect"

	"github.com/rs/zerolog/log"
	hubv1alpha1 "github.com/traefik/hub-agent-kubernetes/pkg/crd/api/hub/v1alpha1"
)

// Updatable represents a object that is updatable.
type Updatable interface {
	Update(polName string)
}

// EventHandler watches ACP resources and calls its set Updatable when they are modified.
type EventHandler struct {
	listener Updatable
}

// NewEventHandler returns a new event handler meant to listen for ACP changes. It calls the given Updatable when an ACP is modified.
func NewEventHandler(listener Updatable) *EventHandler {
	return &EventHandler{
		listener: listener,
	}
}

// OnAdd implements Kubernetes cache.ResourceEventHandler so it can be used as an informer event handler.
func (w *EventHandler) OnAdd(obj interface{}) {
	v, ok := obj.(*hubv1alpha1.AccessControlPolicy)
	if !ok {
		log.Error().
			Str("component", "acp_watcher").
			Str("type", fmt.Sprintf("%T", obj)).
			Msg("Received add event of unknown type")
		return
	}

	w.listener.Update(v.ObjectMeta.Name)
}

// OnUpdate implements Kubernetes cache.ResourceEventHandler so it can be used as an informer event handler.
func (w *EventHandler) OnUpdate(oldObj, newObj interface{}) {
	newACP, ok := newObj.(*hubv1alpha1.AccessControlPolicy)
	if !ok {
		log.Error().
			Str("component", "acp_watcher").
			Str("type", fmt.Sprintf("%T", newObj)).
			Msg("Received update event of unknown type (old)")
		return
	}

	oldACP, ok := oldObj.(*hubv1alpha1.AccessControlPolicy)
	if !ok {
		log.Error().
			Str("component", "acp_watcher").
			Str("type", fmt.Sprintf("%T", oldObj)).
			Msg("Received update event of unknown type (new)")
		return
	}

	if !headersChanged(oldACP.Spec, newACP.Spec) {
		return
	}

	w.listener.Update(newACP.ObjectMeta.Name)
}

// OnDelete implements Kubernetes cache.ResourceEventHandler so it can be used as an informer event handler.
func (w *EventHandler) OnDelete(obj interface{}) {
	v, ok := obj.(*hubv1alpha1.AccessControlPolicy)
	if !ok {
		log.Error().
			Str("component", "acp_watcher").
			Str("type", fmt.Sprintf("%T", obj)).
			Msg("Received delete event of unknown type")
		return
	}

	w.listener.Update(v.ObjectMeta.Name)
}

func headersChanged(oldCfg, newCfg hubv1alpha1.AccessControlPolicySpec) bool {
	switch {
	case newCfg.OIDC != nil:
		if oldCfg.OIDC == nil {
			return true
		}

		return !reflect.DeepEqual(oldCfg.OIDC.ForwardHeaders, newCfg.OIDC.ForwardHeaders)

	case newCfg.JWT != nil:
		if oldCfg.JWT == nil {
			return true
		}

		return !reflect.DeepEqual(oldCfg.JWT.ForwardHeaders, newCfg.JWT.ForwardHeaders) ||
			oldCfg.JWT.StripAuthorizationHeader != newCfg.JWT.StripAuthorizationHeader

	case newCfg.BasicAuth != nil:
		if oldCfg.BasicAuth == nil {
			return true
		}

		return newCfg.BasicAuth.ForwardUsernameHeader != oldCfg.BasicAuth.ForwardUsernameHeader ||
			newCfg.BasicAuth.StripAuthorizationHeader != oldCfg.BasicAuth.StripAuthorizationHeader

	default:
		return false
	}
}

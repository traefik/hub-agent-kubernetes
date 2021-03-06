/*
Copyright The Kubernetes Authors.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

// Code generated by client-gen. DO NOT EDIT.

package fake

import (
	"context"

	v1alpha1 "github.com/traefik/hub-agent-kubernetes/pkg/crd/api/traefik/v1alpha1"
	v1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	labels "k8s.io/apimachinery/pkg/labels"
	schema "k8s.io/apimachinery/pkg/runtime/schema"
	types "k8s.io/apimachinery/pkg/types"
	watch "k8s.io/apimachinery/pkg/watch"
	testing "k8s.io/client-go/testing"
)

// FakeMiddlewares implements MiddlewareInterface
type FakeMiddlewares struct {
	Fake *FakeTraefikV1alpha1
	ns   string
}

var middlewaresResource = schema.GroupVersionResource{Group: "traefik.containo.us", Version: "v1alpha1", Resource: "middlewares"}

var middlewaresKind = schema.GroupVersionKind{Group: "traefik.containo.us", Version: "v1alpha1", Kind: "Middleware"}

// Get takes name of the middleware, and returns the corresponding middleware object, and an error if there is any.
func (c *FakeMiddlewares) Get(ctx context.Context, name string, options v1.GetOptions) (result *v1alpha1.Middleware, err error) {
	obj, err := c.Fake.
		Invokes(testing.NewGetAction(middlewaresResource, c.ns, name), &v1alpha1.Middleware{})

	if obj == nil {
		return nil, err
	}
	return obj.(*v1alpha1.Middleware), err
}

// List takes label and field selectors, and returns the list of Middlewares that match those selectors.
func (c *FakeMiddlewares) List(ctx context.Context, opts v1.ListOptions) (result *v1alpha1.MiddlewareList, err error) {
	obj, err := c.Fake.
		Invokes(testing.NewListAction(middlewaresResource, middlewaresKind, c.ns, opts), &v1alpha1.MiddlewareList{})

	if obj == nil {
		return nil, err
	}

	label, _, _ := testing.ExtractFromListOptions(opts)
	if label == nil {
		label = labels.Everything()
	}
	list := &v1alpha1.MiddlewareList{ListMeta: obj.(*v1alpha1.MiddlewareList).ListMeta}
	for _, item := range obj.(*v1alpha1.MiddlewareList).Items {
		if label.Matches(labels.Set(item.Labels)) {
			list.Items = append(list.Items, item)
		}
	}
	return list, err
}

// Watch returns a watch.Interface that watches the requested middlewares.
func (c *FakeMiddlewares) Watch(ctx context.Context, opts v1.ListOptions) (watch.Interface, error) {
	return c.Fake.
		InvokesWatch(testing.NewWatchAction(middlewaresResource, c.ns, opts))

}

// Create takes the representation of a middleware and creates it.  Returns the server's representation of the middleware, and an error, if there is any.
func (c *FakeMiddlewares) Create(ctx context.Context, middleware *v1alpha1.Middleware, opts v1.CreateOptions) (result *v1alpha1.Middleware, err error) {
	obj, err := c.Fake.
		Invokes(testing.NewCreateAction(middlewaresResource, c.ns, middleware), &v1alpha1.Middleware{})

	if obj == nil {
		return nil, err
	}
	return obj.(*v1alpha1.Middleware), err
}

// Update takes the representation of a middleware and updates it. Returns the server's representation of the middleware, and an error, if there is any.
func (c *FakeMiddlewares) Update(ctx context.Context, middleware *v1alpha1.Middleware, opts v1.UpdateOptions) (result *v1alpha1.Middleware, err error) {
	obj, err := c.Fake.
		Invokes(testing.NewUpdateAction(middlewaresResource, c.ns, middleware), &v1alpha1.Middleware{})

	if obj == nil {
		return nil, err
	}
	return obj.(*v1alpha1.Middleware), err
}

// Delete takes name of the middleware and deletes it. Returns an error if one occurs.
func (c *FakeMiddlewares) Delete(ctx context.Context, name string, opts v1.DeleteOptions) error {
	_, err := c.Fake.
		Invokes(testing.NewDeleteAction(middlewaresResource, c.ns, name), &v1alpha1.Middleware{})

	return err
}

// DeleteCollection deletes a collection of objects.
func (c *FakeMiddlewares) DeleteCollection(ctx context.Context, opts v1.DeleteOptions, listOpts v1.ListOptions) error {
	action := testing.NewDeleteCollectionAction(middlewaresResource, c.ns, listOpts)

	_, err := c.Fake.Invokes(action, &v1alpha1.MiddlewareList{})
	return err
}

// Patch applies the patch and returns the patched middleware.
func (c *FakeMiddlewares) Patch(ctx context.Context, name string, pt types.PatchType, data []byte, opts v1.PatchOptions, subresources ...string) (result *v1alpha1.Middleware, err error) {
	obj, err := c.Fake.
		Invokes(testing.NewPatchSubresourceAction(middlewaresResource, c.ns, name, pt, data, subresources...), &v1alpha1.Middleware{})

	if obj == nil {
		return nil, err
	}
	return obj.(*v1alpha1.Middleware), err
}

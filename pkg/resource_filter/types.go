package resource_filter

import "k8s.io/apimachinery/pkg/runtime/schema"

type cohabitatingResource struct {
	resource       string
	groupResource1 schema.GroupResource
	groupResource2 schema.GroupResource
	seen           bool
}

func newCohabitatingResource(resource, group1, group2 string) *cohabitatingResource {
	return &cohabitatingResource{
		resource:       resource,
		groupResource1: schema.GroupResource{Group: group1, Resource: resource},
		groupResource2: schema.GroupResource{Group: group2, Resource: resource},
		seen:           false,
	}
}

type kubernetesResource struct {
	GroupResource   schema.GroupResource        `json:"groupResource"`
	PreferredGVR    schema.GroupVersionResource `json:"preferredGVR"`
	Namespace       string                      `json:"namespace"`
	Name            string                      `json:"name"`
	Path            string                      `json:"path"`
	OrderedResource bool                        `json:"orderedResource"`
	// set to true during backup processing when added to an ItemBlock
	// or if the item is excluded from backup.
	InItemBlockOrExcluded bool `json:"inItemBlockOrExcluded"`
	// Kind is added to facilitate creating an itemKey for progress tracking
	Kind string `json:"kind"`
}

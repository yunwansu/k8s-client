package resource_filter

import (
	"github.com/sirupsen/logrus"
	"github.com/vmware-tanzu/velero/pkg/kuberesource"
	"github.com/vmware-tanzu/velero/pkg/util/collections"
	corev1api "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/runtime"
)

// nsTracker is used to integrate several namespace filters together.
//  1. Backup's namespace Include/Exclude filters;
//  2. Backup's (Or)LabelSelector selected namespace;
//  3. Backup's (Or)LabelSelector selected resources' namespaces.
//
// Rules:
//
//	a. When backup namespace Include/Exclude filters get everything,
//	The namespaces, which do not have backup including resources,
//	are not collected.
//
//	b. If the namespace I/E filters and the (Or)LabelSelectors selected
//	namespaces are different. The tracker takes the union of them.
type nsTracker struct {
	singleLabelSelector labels.Selector
	orLabelSelector     []labels.Selector
	namespaceFilter     *collections.IncludesExcludes
	logger              logrus.FieldLogger

	namespaceMap map[string]bool
}

// track add the namespace into the namespaceMap.
func (nt *nsTracker) track(ns string) {
	if nt.namespaceMap == nil {
		nt.namespaceMap = make(map[string]bool)
	}

	if _, ok := nt.namespaceMap[ns]; !ok {
		nt.namespaceMap[ns] = true
	}
}

// isTracked check whether the namespace's name exists in
// namespaceMap.
func (nt *nsTracker) isTracked(ns string) bool {
	if nt.namespaceMap != nil {
		return nt.namespaceMap[ns]
	}
	return false
}

// init initialize the namespaceMap, and add elements according to
// namespace include/exclude filters and the backup label selectors.
func (nt *nsTracker) init(
	unstructuredNSs []unstructured.Unstructured,
	singleLabelSelector labels.Selector,
	orLabelSelector []labels.Selector,
	namespaceFilter *collections.IncludesExcludes,
	logger logrus.FieldLogger,
) {
	if nt.namespaceMap == nil {
		nt.namespaceMap = make(map[string]bool)
	}
	nt.singleLabelSelector = singleLabelSelector
	nt.orLabelSelector = orLabelSelector
	nt.namespaceFilter = namespaceFilter
	nt.logger = logger

	for _, namespace := range unstructuredNSs {
		ns := new(corev1api.Namespace)
		if err := runtime.DefaultUnstructuredConverter.FromUnstructured(namespace.UnstructuredContent(), ns); err != nil {
			nt.logger.WithError(err).Errorf("Fail to convert unstructured into namespace %s", namespace.GetName())
			continue
		}
		if ns.Status.Phase != corev1api.NamespaceActive {
			nt.logger.Infof("Skip namespace %s because it's not in Active phase.", namespace.GetName())
			continue
		}

		if nt.singleLabelSelector != nil &&
			nt.singleLabelSelector.Matches(labels.Set(namespace.GetLabels())) {
			nt.logger.Debugf("Track namespace %s, because its labels match backup LabelSelector.",
				namespace.GetName(),
			)

			nt.track(namespace.GetName())
			continue
		}

		if len(nt.orLabelSelector) > 0 {
			for _, selector := range nt.orLabelSelector {
				if selector.Matches(labels.Set(namespace.GetLabels())) {
					nt.logger.Debugf("Track namespace %s, because its labels match the backup OrLabelSelector.",
						namespace.GetName(),
					)
					nt.track(namespace.GetName())
					continue
				}
			}
		}

		// Skip the backup when the backup's namespace filter has
		// default value, and the namespace doesn't match backup
		// LabelSelector and OrLabelSelector.
		// https://github.com/vmware-tanzu/velero/issues/7105
		if nt.namespaceFilter.IncludeEverything() &&
			(nt.singleLabelSelector != nil || len(nt.orLabelSelector) > 0) {
			continue
		}

		if nt.namespaceFilter.ShouldInclude(namespace.GetName()) {
			nt.logger.Debugf("Track namespace %s, because its name match the backup namespace filter.",
				namespace.GetName(),
			)
			nt.track(namespace.GetName())
		}
	}
}

// filterNamespaces filters the input resource list to remove the
// namespaces not tracked by the nsTracker.
func (nt *nsTracker) filterNamespaces(
	resources []*kubernetesResource,
) []*kubernetesResource {
	result := make([]*kubernetesResource, 0)

	for _, resource := range resources {
		if resource.GroupResource != kuberesource.Namespaces ||
			nt.isTracked(resource.Name) {
			result = append(result, resource)
		}
	}

	return result
}

package resource_filter

import (
	"fmt"
	"github.com/pkg/errors"
	"github.com/sirupsen/logrus"
	pkgbackup "github.com/vmware-tanzu/velero/pkg/backup"
	"github.com/vmware-tanzu/velero/pkg/client"
	veleroDiscovery "github.com/vmware-tanzu/velero/pkg/discovery"
	"github.com/vmware-tanzu/velero/pkg/kuberesource"
	"golang.org/x/net/context"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/client-go/tools/pager"
)

// itemCollector collects items from the Kubernetes API according to
// the backup spec and writes them to files inside dir.
type itemCollector struct {
	log                   logrus.FieldLogger
	backupRequest         *pkgbackup.Request
	discoveryHelper       veleroDiscovery.Helper
	dynamicFactory        client.DynamicFactory
	cohabitatingResources map[string]*cohabitatingResource
	pageSize              int
	nsTracker             nsTracker
}

// getAllItems gets all backup-relevant items from all API groups.
func (r *itemCollector) getAllItems() []*kubernetesResource {
	var resources []*kubernetesResource
	for _, group := range r.discoveryHelper.Resources() {
		groupItems, err := r.getObjectsByGroup(r.log, group)
		if err != nil {
			r.log.WithError(err).WithField("apiGroup", group.String()).
				Error("Error collecting resources from API group")
			continue
		}

		resources = append(resources, groupItems...)
	}
	return r.nsTracker.filterNamespaces(resources)
}

// getObjectsByGroup collects all relevant items from a single API group.
// If resourceIDsMap is supplied, then only those items are returned,
// with GVR/APIResource metadata supplied.
func (r *itemCollector) getObjectsByGroup(
	log logrus.FieldLogger,
	group *metav1.APIResourceList,
) ([]*kubernetesResource, error) {
	log = log.WithField("group", group.GroupVersion)

	log.Infof("Getting items for group")

	// Parse so we can check if this is the core group
	gv, err := schema.ParseGroupVersion(group.GroupVersion)
	if err != nil {
		return nil, errors.Wrapf(err, "error parsing GroupVersion %q",
			group.GroupVersion)
	}

	var items []*kubernetesResource
	for _, resource := range group.APIResources {
		resourceItems, err := r.getObjects(log, gv, resource)
		if err != nil {
			log.WithError(err).WithField("resource", resource.String()).
				Error("Error getting items for resource")
			continue
		}

		items = append(items, resourceItems...)
	}

	return items, nil
}

// getObjects collects all relevant items for a given group-version-resource.
// If resourceIDsMap is supplied, the items will be pulled from here
// rather than from the cluster and applying include/exclude rules.
func (r *itemCollector) getObjects(
	log logrus.FieldLogger,
	gv schema.GroupVersion,
	resource metav1.APIResource,
) ([]*kubernetesResource, error) {
	log = log.WithField("resource", resource.Name)

	log.Info("Getting items for resource")

	var (
		gvr = gv.WithResource(resource.Name)
		gr  = gvr.GroupResource()
	)

	// Getting the preferred group version of this resource
	preferredGVR, _, err := r.discoveryHelper.ResourceFor(gr.WithVersion(""))
	if err != nil {
		return nil, errors.WithStack(err)
	}

	if !r.backupRequest.ResourceIncludesExcludes.ShouldInclude(gr.String()) {
		log.Infof("Skipping resource because it's excluded")
		return nil, nil
	}

	if cohabitator, found := r.cohabitatingResources[resource.Name]; found {
		if gv.Group == cohabitator.groupResource1.Group ||
			gv.Group == cohabitator.groupResource2.Group {
			if cohabitator.seen {
				log.WithFields(
					logrus.Fields{
						"cohabitatingResource1": cohabitator.groupResource1.String(),
						"cohabitatingResource2": cohabitator.groupResource2.String(),
					},
				).Infof("Skipping resource because it cohabitates and we've already processed it")
				return nil, nil
			}
			cohabitator.seen = true
		}
	}

	// Handle namespace resource here.
	// Namespace are filtered by namespace include/exclude filters,
	// backup LabelSelectors and OrLabelSelectors are checked too.
	if gr == kuberesource.Namespaces {
		return r.collectNamespaces(
			resource,
			gv,
			gr,
			preferredGVR,
			log,
		)
	}

	clusterScoped := !resource.Namespaced
	namespacesToList := getNamespacesToList(r.backupRequest.NamespaceIncludesExcludes)

	// If we get here, we're backing up something other than namespaces
	if clusterScoped {
		namespacesToList = []string{""}
	}

	var items []*kubernetesResource

	for _, namespace := range namespacesToList {
		unstructuredItems, err := r.listResourceByLabelsPerNamespace(
			namespace, gr, gv, resource, log)
		if err != nil {
			continue
		}

		// Collect items in included Namespaces
		for i := range unstructuredItems {
			item := &unstructuredItems[i]

			items = append(items, &kubernetesResource{
				GroupResource: gr,
				PreferredGVR:  preferredGVR,
				Namespace:     item.GetNamespace(),
				Name:          item.GetName(),
				//Path:          Path,
				Kind: resource.Kind,
			})

			if item.GetNamespace() != "" {
				log.Debugf("Track namespace %s in nsTracker", item.GetNamespace())
				r.nsTracker.track(item.GetNamespace())
			}
		}
	}

	return items, nil
}

// collectNamespaces process namespace resource according to namespace filters.
func (r *itemCollector) collectNamespaces(
	resource metav1.APIResource,
	gv schema.GroupVersion,
	gr schema.GroupResource,
	preferredGVR schema.GroupVersionResource,
	log logrus.FieldLogger,
) ([]*kubernetesResource, error) {
	resourceClient, err := r.dynamicFactory.ClientForGroupVersionResource(gv, resource, "")
	if err != nil {
		log.WithError(err).Error("Error getting dynamic client")
		return nil, errors.WithStack(err)
	}

	unstructuredList, err := resourceClient.List(metav1.ListOptions{})
	if err != nil {
		log.WithError(errors.WithStack(err)).Error("error list namespaces")
		return nil, errors.WithStack(err)
	}

	for _, includedNSName := range r.backupRequest.Backup.Spec.IncludedNamespaces {
		nsExists := false
		// Skip checking the namespace existing when it's "*".
		if includedNSName == "*" {
			continue
		}
		for _, unstructuredNS := range unstructuredList.Items {
			if unstructuredNS.GetName() == includedNSName {
				nsExists = true
			}
		}

		if !nsExists {
			log.Errorf("fail to get the namespace %s specified in backup.Spec.IncludedNamespaces", includedNSName)
		}
	}

	var singleSelector labels.Selector
	var orSelectors []labels.Selector

	if r.backupRequest.Backup.Spec.LabelSelector != nil {
		var err error
		singleSelector, err = metav1.LabelSelectorAsSelector(
			r.backupRequest.Backup.Spec.LabelSelector)
		if err != nil {
			log.WithError(err).Errorf("Fail to convert backup LabelSelector %s into selector.",
				metav1.FormatLabelSelector(r.backupRequest.Backup.Spec.LabelSelector))
		}
	}
	if r.backupRequest.Backup.Spec.OrLabelSelectors != nil {
		for _, ls := range r.backupRequest.Backup.Spec.OrLabelSelectors {
			orSelector, err := metav1.LabelSelectorAsSelector(ls)
			if err != nil {
				log.WithError(err).Errorf("Fail to convert backup OrLabelSelector %s into selector.",
					metav1.FormatLabelSelector(ls))
			}
			orSelectors = append(orSelectors, orSelector)
		}
	}

	r.nsTracker.init(
		unstructuredList.Items,
		singleSelector,
		orSelectors,
		r.backupRequest.NamespaceIncludesExcludes,
		log,
	)

	var items []*kubernetesResource

	for index := range unstructuredList.Items {
		items = append(items, &kubernetesResource{
			GroupResource: gr,
			PreferredGVR:  preferredGVR,
			Name:          unstructuredList.Items[index].GetName(),
			Kind:          resource.Kind,
		})
	}

	return items, nil
}

func (r *itemCollector) listResourceByLabelsPerNamespace(
	namespace string,
	gr schema.GroupResource,
	gv schema.GroupVersion,
	resource metav1.APIResource,
	logger logrus.FieldLogger,
) ([]unstructured.Unstructured, error) {
	// List items from Kubernetes API
	logger = logger.WithField("namespace", namespace)

	resourceClient, err := r.dynamicFactory.ClientForGroupVersionResource(gv, resource, namespace)
	if err != nil {
		logger.WithError(err).Error("Error getting dynamic client")
		return nil, err
	}

	var orLabelSelectors []string
	if r.backupRequest.Spec.OrLabelSelectors != nil {
		for _, s := range r.backupRequest.Spec.OrLabelSelectors {
			orLabelSelectors = append(orLabelSelectors, metav1.FormatLabelSelector(s))
		}
	} else {
		orLabelSelectors = []string{}
	}

	logger.Info("Listing items")
	unstructuredItems := make([]unstructured.Unstructured, 0)

	// Listing items for orLabelSelectors
	errListingForNS := false
	for _, label := range orLabelSelectors {
		unstructuredItems, err = r.listItemsForLabel(unstructuredItems, gr, label, resourceClient)
		if err != nil {
			errListingForNS = true
		}
	}

	if errListingForNS {
		logger.WithError(err).Error("Error listing items")
		return nil, err
	}

	var labelSelector string
	if selector := r.backupRequest.Spec.LabelSelector; selector != nil {
		labelSelector = metav1.FormatLabelSelector(selector)
	}

	// Listing items for labelSelector (singular)
	if len(orLabelSelectors) == 0 {
		unstructuredItems, err = r.listItemsForLabel(
			unstructuredItems,
			gr,
			labelSelector,
			resourceClient,
		)
		if err != nil {
			logger.WithError(err).Error("Error listing items")
			return nil, err
		}
	}

	logger.Infof("Retrieved %d items", len(unstructuredItems))
	return unstructuredItems, nil
}

func (r *itemCollector) listItemsForLabel(
	unstructuredItems []unstructured.Unstructured,
	gr schema.GroupResource,
	label string,
	resourceClient client.Dynamic,
) ([]unstructured.Unstructured, error) {
	if r.pageSize > 0 {
		// process pager client calls
		list, err := r.processPagerClientCalls(gr, label, resourceClient)
		if err != nil {
			return unstructuredItems, err
		}

		err = meta.EachListItem(list, func(object runtime.Object) error {
			u, ok := object.(*unstructured.Unstructured)
			if !ok {
				r.log.WithError(errors.WithStack(fmt.Errorf("expected *unstructured.Unstructured but got %T", u))).
					Error("unable to understand entry in the list")
				return fmt.Errorf("expected *unstructured.Unstructured but got %T", u)
			}
			unstructuredItems = append(unstructuredItems, *u)
			return nil
		})
		if err != nil {
			r.log.WithError(errors.WithStack(err)).Error("unable to understand paginated list")
			return unstructuredItems, err
		}
	} else {
		unstructuredList, err := resourceClient.List(metav1.ListOptions{LabelSelector: label})
		if err != nil {
			r.log.WithError(errors.WithStack(err)).Error("Error listing items")
			return unstructuredItems, err
		}
		unstructuredItems = append(unstructuredItems, unstructuredList.Items...)
	}
	return unstructuredItems, nil
}

// function to process pager client calls when the pageSize is specified
func (r *itemCollector) processPagerClientCalls(
	gr schema.GroupResource,
	label string,
	resourceClient client.Dynamic,
) (runtime.Object, error) {
	// If limit is positive, use a pager to split list over multiple requests
	// Use Velero's dynamic list function instead of the default
	listPager := pager.New(pager.SimplePageFunc(func(opts metav1.ListOptions) (runtime.Object, error) {
		return resourceClient.List(opts)
	}))
	// Use the page size defined in the server config
	// TODO allow configuration of page buffer size
	listPager.PageSize = int64(r.pageSize)
	// Add each item to temporary slice
	list, paginated, err := listPager.List(context.Background(), metav1.ListOptions{LabelSelector: label})

	if err != nil {
		r.log.WithError(errors.WithStack(err)).Error("Error listing resources")
		return list, err
	}

	if !paginated {
		r.log.Infof("list for groupResource %s was not paginated", gr)
	}

	return list, nil
}

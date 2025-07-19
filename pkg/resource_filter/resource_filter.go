package resource_filter

import (
	"fmt"
	"github.com/sirupsen/logrus"
	v1 "github.com/vmware-tanzu/velero/pkg/apis/velero/v1"
	pkgbackup "github.com/vmware-tanzu/velero/pkg/backup"
	"github.com/vmware-tanzu/velero/pkg/client"
	veleroConfig "github.com/vmware-tanzu/velero/pkg/cmd/server/config"
	veleroDiscovery "github.com/vmware-tanzu/velero/pkg/discovery"
	"github.com/vmware-tanzu/velero/pkg/util/collections"
	"github.com/vmware-tanzu/velero/pkg/util/logging"
	"golang.org/x/net/context"
	"k8s.io/apimachinery/pkg/util/sets"
	"k8s.io/apimachinery/pkg/util/wait"
	"k8s.io/client-go/discovery"
	"k8s.io/client-go/dynamic"
	"k8s.io/client-go/rest"
	"slices"
	"time"
)

var autoExcludeNamespaceScopedResources = []string{
	// CSI VolumeSnapshot and VolumeSnapshotContent are intermediate resources.
	// Velero only handle the VS and VSC created during backup,
	// not during resource collecting.
	"volumesnapshots.snapshot.storage.k8s.io",
}

var autoExcludeClusterScopedResources = []string{
	// CSI VolumeSnapshot and VolumeSnapshotContent are intermediate resources.
	// Velero only handle the VS and VSC created during backup,
	// not during resource collecting.
	"volumesnapshotcontents.snapshot.storage.k8s.io",
}

type ResourceFilter struct {
	ctx             context.Context
	dynamicClient   *dynamic.DynamicClient
	discoveryHelper veleroDiscovery.Helper
	logger          *logrus.Logger
}

func NewResourceFilter(ctx context.Context, config *rest.Config) (*ResourceFilter, error) {
	vconf := veleroConfig.GetDefaultConfig()
	logLevel := vconf.LogLevel.Parse()
	format := vconf.LogFormat.Parse()
	logger := logging.DefaultLogger(logLevel, format)

	dynamicClient, err := dynamic.NewForConfig(config)
	if err != nil {
		return nil, err
	}

	discoveryClient, err := discovery.NewDiscoveryClientForConfig(config)
	if err != nil {
		return nil, err
	}

	discoveryHelper, err := veleroDiscovery.NewHelper(discoveryClient, logger)
	if err != nil {
		return nil, err
	}

	go wait.Until(
		func() {
			if err := discoveryHelper.Refresh(); err != nil {
				logger.WithError(err).Error("Error refreshing discovery")
			}
		},
		5*time.Minute,
		ctx.Done(),
	)

	return &ResourceFilter{
		ctx:             ctx,
		dynamicClient:   dynamicClient,
		discoveryHelper: discoveryHelper,
		logger:          logger,
	}, nil
}

func (r *ResourceFilter) GetAllItems(backup *v1.Backup) []*kubernetesResource {
	request := pkgbackup.Request{
		Backup: backup.DeepCopy(),
	}

	// validate whether Included/Excluded resources and IncludedClusterResource are mixed with
	// Included/Excluded cluster-scoped/namespace-scoped resources.
	if oldAndNewFilterParametersUsedTogether(request.Spec) {
		validatedError := fmt.Sprintf("include-resources, exclude-resources and include-cluster-resources are old filter parameters.\n" +
			"include-cluster-scoped-resources, exclude-cluster-scoped-resources, include-namespace-scoped-resources and exclude-namespace-scoped-resources are new filter parameters.\n" +
			"They cannot be used together")
		request.Status.ValidationErrors = append(request.Status.ValidationErrors, validatedError)
	}

	if collections.UseOldResourceFilters(request.Spec) {
		// validate the included/excluded resources
		ieErr := collections.ValidateIncludesExcludes(request.Spec.IncludedResources, request.Spec.ExcludedResources)
		if len(ieErr) > 0 {
			for _, err := range ieErr {
				request.Status.ValidationErrors = append(request.Status.ValidationErrors, fmt.Sprintf("Invalid included/excluded resource lists: %v", err))
			}
		} else {
			request.Spec.IncludedResources, request.Spec.ExcludedResources =
				modifyResourceIncludeExclude(
					request.Spec.IncludedResources,
					request.Spec.ExcludedResources,
					append(autoExcludeNamespaceScopedResources, autoExcludeClusterScopedResources...),
				)
		}
	} else {
		// validate the cluster-scoped included/excluded resources
		clusterErr := collections.ValidateScopedIncludesExcludes(request.Spec.IncludedClusterScopedResources, request.Spec.ExcludedClusterScopedResources)
		if len(clusterErr) > 0 {
			for _, err := range clusterErr {
				request.Status.ValidationErrors = append(request.Status.ValidationErrors, fmt.Sprintf("Invalid cluster-scoped included/excluded resource lists: %s", err))
			}
		} else {
			request.Spec.IncludedClusterScopedResources, request.Spec.ExcludedClusterScopedResources =
				modifyResourceIncludeExclude(
					request.Spec.IncludedClusterScopedResources,
					request.Spec.ExcludedClusterScopedResources,
					autoExcludeClusterScopedResources,
				)
		}

		// validate the namespace-scoped included/excluded resources
		namespaceErr := collections.ValidateScopedIncludesExcludes(request.Spec.IncludedNamespaceScopedResources, request.Spec.ExcludedNamespaceScopedResources)
		if len(namespaceErr) > 0 {
			for _, err := range namespaceErr {
				request.Status.ValidationErrors = append(request.Status.ValidationErrors, fmt.Sprintf("Invalid namespace-scoped included/excluded resource lists: %s", err))
			}
		} else {
			request.Spec.IncludedNamespaceScopedResources, request.Spec.ExcludedNamespaceScopedResources =
				modifyResourceIncludeExclude(
					request.Spec.IncludedNamespaceScopedResources,
					request.Spec.ExcludedNamespaceScopedResources,
					autoExcludeNamespaceScopedResources,
				)
		}
	}

	// validate the included/excluded namespaces
	for _, err := range collections.ValidateNamespaceIncludesExcludes(request.Spec.IncludedNamespaces, request.Spec.ExcludedNamespaces) {
		request.Status.ValidationErrors = append(request.Status.ValidationErrors, fmt.Sprintf("Invalid included/excluded namespace lists: %v", err))
	}

	// validate that only one exists orLabelSelector or just labelSelector (singular)
	if request.Spec.OrLabelSelectors != nil && request.Spec.LabelSelector != nil {
		request.Status.ValidationErrors = append(request.Status.ValidationErrors, "encountered labelSelector as well as orLabelSelectors in backup spec, only one can be specified")
	}

	request.NamespaceIncludesExcludes = getNamespaceIncludesExcludes(request.Backup)
	r.logger.Infof("Including namespaces: %s", request.NamespaceIncludesExcludes.IncludesString())
	r.logger.Infof("Excluding namespaces: %s", request.NamespaceIncludesExcludes.ExcludesString())

	if collections.UseOldResourceFilters(request.Spec) {
		request.ResourceIncludesExcludes = collections.GetGlobalResourceIncludesExcludes(r.discoveryHelper, r.logger,
			request.Spec.IncludedResources,
			request.Spec.ExcludedResources,
			request.Spec.IncludeClusterResources,
			*request.NamespaceIncludesExcludes)
	} else {
		request.ResourceIncludesExcludes = collections.GetScopeResourceIncludesExcludes(r.discoveryHelper, r.logger,
			request.Spec.IncludedNamespaceScopedResources,
			request.Spec.ExcludedNamespaceScopedResources,
			request.Spec.IncludedClusterScopedResources,
			request.Spec.ExcludedClusterScopedResources,
			*request.NamespaceIncludesExcludes,
		)
	}

	collector := &itemCollector{
		log:                   r.logger,
		backupRequest:         &request,
		discoveryHelper:       r.discoveryHelper,
		dynamicFactory:        client.NewDynamicFactory(r.dynamicClient),
		cohabitatingResources: cohabitatingResources(),
		//dir:                   tempDir,
		pageSize: 30,
	}

	return collector.getAllItems()
}

func cohabitatingResources() map[string]*cohabitatingResource {
	return map[string]*cohabitatingResource{
		"deployments":     newCohabitatingResource("deployments", "extensions", "apps"),
		"daemonsets":      newCohabitatingResource("daemonsets", "extensions", "apps"),
		"replicasets":     newCohabitatingResource("replicasets", "extensions", "apps"),
		"networkpolicies": newCohabitatingResource("networkpolicies", "extensions", "networking.k8s.io"),
		"events":          newCohabitatingResource("events", "", "events.k8s.io"),
	}
}

// getNamespacesToList examines ie and resolves the includes and excludes to a full list of
// namespaces to list. If ie is nil or it includes *, the result is just "" (list across all
// namespaces). Otherwise, the result is a list of every included namespace minus all excluded ones.
func getNamespacesToList(ie *collections.IncludesExcludes) []string {
	if ie == nil {
		return []string{""}
	}

	if ie.ShouldInclude("*") {
		// "" means all namespaces
		return []string{""}
	}

	var list []string
	for _, i := range ie.GetIncludes() {
		if ie.ShouldInclude(i) {
			list = append(list, i)
		}
	}

	return list
}

func oldAndNewFilterParametersUsedTogether(backupSpec v1.BackupSpec) bool {
	haveOldResourceFilterParameters := len(backupSpec.IncludedResources) > 0 ||
		(len(backupSpec.ExcludedResources) > 0) ||
		(backupSpec.IncludeClusterResources != nil)
	haveNewResourceFilterParameters := len(backupSpec.IncludedClusterScopedResources) > 0 ||
		(len(backupSpec.ExcludedClusterScopedResources) > 0) ||
		(len(backupSpec.IncludedNamespaceScopedResources) > 0) ||
		(len(backupSpec.ExcludedNamespaceScopedResources) > 0)

	return haveOldResourceFilterParameters && haveNewResourceFilterParameters
}

func modifyResourceIncludeExclude(include, exclude, addedExclude []string) (modifiedInclude, modifiedExclude []string) {
	modifiedInclude = include
	modifiedExclude = exclude

	excludeStrSet := sets.NewString(exclude...)
	for _, ex := range addedExclude {
		if !excludeStrSet.Has(ex) {
			modifiedExclude = append(modifiedExclude, ex)
		}
	}

	for _, exElem := range modifiedExclude {
		for inIndex, inElem := range modifiedInclude {
			if inElem == exElem {
				modifiedInclude = slices.Delete(modifiedInclude, inIndex, inIndex+1)
			}
		}
	}

	return modifiedInclude, modifiedExclude
}

// getNamespaceIncludesExcludes returns an IncludesExcludes list containing which namespaces to
// include and exclude from the backup.
func getNamespaceIncludesExcludes(backup *v1.Backup) *collections.IncludesExcludes {
	return collections.NewIncludesExcludes().Includes(backup.Spec.IncludedNamespaces...).Excludes(backup.Spec.ExcludedNamespaces...)
}

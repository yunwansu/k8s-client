package main

import (
	"fmt"
	v1 "github.com/vmware-tanzu/velero/pkg/apis/velero/v1"
	"golang.org/x/net/context"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/clientcmd"
	"k8s.io/client-go/util/homedir"
	"path/filepath"
)

func main() {
	config, err := getConfig()
	if err != nil {
		panic(err.Error())
	}

	ctx, cancelFunc := context.WithCancel(context.Background())
	defer cancelFunc()

	resourceFilter, err := NewResourceFilter(ctx, config)
	if err != nil {
		panic(err.Error())
	}

	backup := v1.Backup{
		Spec: v1.BackupSpec{
			IncludedNamespaces:               []string{"kube-system"},
			IncludedNamespaceScopedResources: []string{"persistentvolumeclaims"},
			//IncludedClusterScopedResources:   []string{"pv"},
		},
	}

	items := resourceFilter.GetAllItems(backup.DeepCopy())
	for _, item := range items {
		fmt.Println(item)
	}
}

func getConfig() (*rest.Config, error) {
	kubeconfigPath := ""
	if home := homedir.HomeDir(); home != "" {
		kubeconfigPath = filepath.Join(home, ".kube", "config")
	}

	return clientcmd.BuildConfigFromFlags("", kubeconfigPath)
}

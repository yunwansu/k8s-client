package example

import (
	"context"
	"fmt"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/runtime/schema"
	memory "k8s.io/client-go/discovery/cached"
	"k8s.io/client-go/dynamic"
	"k8s.io/client-go/dynamic/dynamicinformer"
	"k8s.io/client-go/informers"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/restmapper"
	"time"
)

type ClientGoExample struct {
	config *rest.Config
}

func NewClientGoExample(config *rest.Config) *ClientGoExample {
	return &ClientGoExample{
		config: config,
	}
}

func (c *ClientGoExample) runExample() {
	// create the clientset
	clientset, err := kubernetes.NewForConfig(c.config)
	if err != nil {
		panic(err.Error())
	}

	deployments, err := clientset.AppsV1().Deployments("kube-system").List(context.TODO(), metav1.ListOptions{})
	if err != nil {
		panic(err.Error())
	}

	for _, deployment := range deployments.Items {
		fmt.Println(deployment.Namespace, "/", deployment.Name)
	}

	dc, err := dynamic.NewForConfig(c.config)
	if err != nil {
		panic(err.Error())
	}

	mapper := restmapper.NewDeferredDiscoveryRESTMapper(memory.NewMemCacheClient(clientset.Discovery()))

	certManagerGVK := schema.GroupVersionKind{
		Group:   "cert-manager.io",
		Version: "v1",
		Kind:    "CertificateRequest",
	}

	certManagerGVR, err := mapper.RESTMapping(certManagerGVK.GroupKind(), certManagerGVK.Version)
	if err != nil {
		panic(err.Error())
	}

	crs, err := dc.Resource(certManagerGVR.Resource).List(context.TODO(), metav1.ListOptions{})
	if err != nil {
		panic(err.Error())
	}

	for _, cr := range crs.Items {
		fmt.Println(cr.GetNamespace(), "/", cr.GetName())
	}

	factory := informers.NewSharedInformerFactory(clientset, 10*time.Hour)
	podLister := factory.Core().V1().Pods().Lister()

	stopCh := make(chan struct{})
	factory.Start(stopCh)
	factory.WaitForCacheSync(stopCh)

	pods, err := podLister.Pods("kube-system").List(labels.Everything())
	if err != nil {
		panic(err.Error())
	}

	for _, pod := range pods {
		fmt.Printf("- %s/%s\n", pod.Namespace, pod.Name)
	}

	dfactory := dynamicinformer.NewDynamicSharedInformerFactory(dc, 10*time.Hour)
	configMapInformer := dfactory.ForResource(schema.GroupVersionResource{Group: "", Version: "v1", Resource: "configmaps"})
	configMapLister := configMapInformer.Lister()
	dfactory.Start(stopCh)
	dfactory.WaitForCacheSync(stopCh)

	configMaps, err := configMapLister.List(labels.Everything())
	if err != nil {
		panic(err.Error())
	}

	for _, configMap := range configMaps {
		u := configMap.(*unstructured.Unstructured)
		fmt.Printf("- %s/%s\n", u.GetNamespace(), u.GetName())
	}
}

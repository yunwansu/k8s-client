package main

import (
	"fmt"
	"golang.org/x/net/context"
	v1 "k8s.io/api/core/v1"
	"k8s.io/client-go/rest"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/cache"
	"sigs.k8s.io/controller-runtime/pkg/manager"
)

type ControllerRuntimeExample struct {
	config *rest.Config
}

func NewControllerRuntimeExample(config *rest.Config) *ControllerRuntimeExample {
	return &ControllerRuntimeExample{
		config: config,
	}
}

func (c *ControllerRuntimeExample) runControllerRuntimeExample() {
	mgr, err := ctrl.NewManager(c.config, ctrl.Options{
		// 0.16.0+
		Cache: cache.Options{
			DefaultNamespaces: map[string]cache.Config{
				"kube-system": {},
			},
		},
	})
	if err != nil {
		panic(err)
	}
	mgr.Add(manager.RunnableFunc(func(ctx context.Context) error {
		c := mgr.GetClient() // get cache client

		podList := &v1.PodList{}
		err = c.List(context.TODO(), podList)
		if err != nil {
			panic(err)
		}

		for _, pod := range podList.Items {
			fmt.Println(pod.Name)
		}

		return nil
	}))

	if err = ctrl.
		NewControllerManagedBy(mgr).
		For(&v1.Pod{}).
		Complete(&PodReconciler{mgr.GetClient()}); err != nil {
		panic(err)
	}
	mgr.Start(ctrl.SetupSignalHandler())
}

package main

import (
	"fmt"
	"golang.org/x/net/context"
	v1 "k8s.io/api/core/v1"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

type PodReconciler struct {
	client.Client
}

func (r *PodReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	pod := &v1.Pod{}
	err := r.Get(ctx, req.NamespacedName, pod)
	if err != nil {
		return ctrl.Result{}, err
	}

	fmt.Printf("- %s/%s\n", pod.Namespace, pod.Name)
	return ctrl.Result{}, nil
}

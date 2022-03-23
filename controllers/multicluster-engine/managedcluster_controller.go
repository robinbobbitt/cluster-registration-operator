// Copyright Red Hat

package multiclusterengine

import (
	"context"

	"github.com/go-logr/logr"
	// corev1 "k8s.io/api/core/v1"
	apiextensionsclient "k8s.io/apiextensions-apiserver/pkg/client/clientset/clientset"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/dynamic"
	"k8s.io/client-go/kubernetes"
	clusterapiv1 "open-cluster-management.io/api/cluster/v1"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/cluster"
	"sigs.k8s.io/controller-runtime/pkg/handler"
	"sigs.k8s.io/controller-runtime/pkg/source"
)

// ManagedClusterReconciler reconciles a ManagedCluster object
type ManagedClusterReconciler struct {
	client.Client
	KubeClient         kubernetes.Interface
	DynamicClient      dynamic.Interface
	APIExtensionClient apiextensionsclient.Interface
	Log                logr.Logger
	Scheme             *runtime.Scheme
}

func (r *ManagedClusterReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	_ = context.Background()
	_ = r.Log.WithValues("namespace", req.NamespacedName, "name", req.Name)

	return ctrl.Result{}, nil
}

// SetupWithManager sets up the controller with the Manager.
func (r *ManagedClusterReconciler) SetupWithManager(mgr ctrl.Manager, mceCluster cluster.Cluster) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(source.NewKindWithCache(&clusterapiv1.ManagedCluster{}, mceCluster.GetCache())). // Error because cannot use SyncingSource as Object
		Watches(source.NewKindWithCache(&clusterapiv1.ManagedCluster{}, mceCluster.GetCache()),
			&handler.EnqueueRequestForObject{}).
		Complete(r)
}

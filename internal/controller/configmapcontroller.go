package controller

import (
	"context"
	"fmt"
	"time"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/runtime"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/builder"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/event"
	"sigs.k8s.io/controller-runtime/pkg/predicate"
)

const (
	ConfigMapReconcilerName = "configmap-reconciler"
)

// ConfigMapReconciler reconciles ConfigMap resources in a Kubernetes cluster.
type ConfigMapReconciler struct {
	BaseReconciler
}

// NewConfigMapReconciler creates a new ConfigMapReconciler instance.
func NewConfigMapReconciler(client client.Client, scheme *runtime.Scheme) *ConfigMapReconciler {
	return &ConfigMapReconciler{
		BaseReconciler: BaseReconciler{
			Client: client,
			Scheme: scheme,
		},
	}
}

// +kubebuilder:rbac:groups=core,resources=configmaps,verbs=get;list;watch;patch
// +kubebuilder:rbac:groups=helm.toolkit.fluxcd.io,resources=helmreleases,verbs=get;list;watch;patch

func (r *ConfigMapReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	fmt.Println("Reconciling ConfigMap:", req.NamespacedName)
	configMap := &corev1.ConfigMap{}
	return r.ReconcileResource(ctx, req, configMap)
}

func (r *ConfigMapReconciler) SetupWithManager(
	mgr ctrl.Manager, maxConcurrency int, cacheSyncTimeout time.Duration) error {

	return ctrl.NewControllerManagedBy(mgr).
		For(&corev1.ConfigMap{}, builder.WithPredicates(predicate.Funcs{
			CreateFunc: func(e event.CreateEvent) bool {
				configMap, ok := e.Object.(*corev1.ConfigMap)
				if !ok {
					return false
				}

				return isValidConfigMap(configMap)
			},

			UpdateFunc: func(e event.UpdateEvent) bool {
				configMapOld, okOld := e.ObjectOld.(*corev1.ConfigMap)
				configMapNew, okNew := e.ObjectNew.(*corev1.ConfigMap)

				if !okOld || !okNew || !isValidConfigMap(configMapNew) {
					return false
				}

				oldHash := getDataHashCM(configMapOld.Data)
				newHash := getDataHashCM(configMapNew.Data)

				return oldHash != newHash
			},
			DeleteFunc: func(e event.DeleteEvent) bool {
				return false
			},
			GenericFunc: func(e event.GenericEvent) bool {
				configMap, ok := e.Object.(*corev1.ConfigMap)
				if !ok {
					return false
				}

				return isValidConfigMap(configMap)
			},
		})).
		WithOptions(ControllerOptions(maxConcurrency, cacheSyncTimeout)).
		Complete(r)
}

func isValidConfigMap(cm *corev1.ConfigMap) bool {
	if v, ok := cm.Labels[LabelReconcilerNameSourceKey]; ok {
		return v == "true"
	}
	return false
}

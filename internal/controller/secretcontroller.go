package controller

import (
	"context"
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
	SecretReconcilerName = "secret-reconciler"
)

// SecretReconciler reconciles Secret resources in a Kubernetes cluster.
type SecretReconciler struct {
	BaseReconciler
}

// NewSecretReconciler creates a new SecretReconciler instance.
func NewSecretReconciler(client client.Client, scheme *runtime.Scheme) *SecretReconciler {
	return &SecretReconciler{
		BaseReconciler: BaseReconciler{
			Client: client,
			Scheme: scheme,
		},
	}
}

// +kubebuilder:rbac:groups=core,resources=secrets,verbs=get;list;watch;patch
// +kubebuilder:rbac:groups=helm.toolkit.fluxcd.io,resources=helmreleases,verbs=get;list;watch;patch

func (r *SecretReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	secret := &corev1.Secret{}
	return r.ReconcileResource(ctx, req, secret)
}

func (r *SecretReconciler) SetupWithManager(
	mgr ctrl.Manager, maxConcurrency int, cacheSyncTimeout time.Duration) error {

	return ctrl.NewControllerManagedBy(mgr).
		For(&corev1.Secret{}, builder.WithPredicates(predicate.Funcs{
			CreateFunc: func(e event.CreateEvent) bool {
				secret, ok := e.Object.(*corev1.Secret)
				if !ok {
					return false
				}

				return isValidSecret(secret)
			},
			UpdateFunc: func(e event.UpdateEvent) bool {
				secretOld, okOld := e.ObjectOld.(*corev1.Secret)
				secretNew, okNew := e.ObjectNew.(*corev1.Secret)
				if !okOld || !okNew || !isValidSecret(secretNew) {
					return false
				}

				oldHash := getDataHashSecret(secretOld.Data)
				newHash := getDataHashSecret(secretNew.Data)

				return oldHash != newHash
			},
			DeleteFunc: func(e event.DeleteEvent) bool {
				return false
			},
			GenericFunc: func(e event.GenericEvent) bool {
				secret, ok := e.Object.(*corev1.Secret)
				if !ok {
					return false
				}

				return isValidSecret(secret)
			},
		})).
		WithOptions(ControllerOptions(maxConcurrency, cacheSyncTimeout)).
		Complete(r)
}

func isValidSecret(secret *corev1.Secret) bool {
	if v, ok := secret.Labels[LabelReconcilerNameSourceKey]; ok {
		return v == "true"
	}
	return false
}

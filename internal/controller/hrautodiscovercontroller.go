package controller

import (
	"context"
	"fmt"
	"time"

	helmv2 "github.com/fluxcd/helm-controller/api/v2"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/builder"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/event"
	"sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/predicate"
)

const (
	HRAutodicoverReconcilerName = "hr-autodiscovery"
)

type HRAutodicoverReconciler struct {
	client.Client
	Scheme *runtime.Scheme
}

// NewHRAutodicoverReconciler creates a new HRAutodicoverReconciler instance.
func NewHRAutodicoverReconciler(client client.Client, scheme *runtime.Scheme) *HRAutodicoverReconciler {
	return &HRAutodicoverReconciler{
		Client: client,
		Scheme: scheme,
	}
}

// +kubebuilder:rbac:groups=helm.toolkit.fluxcd.io,resources=helmreleases,verbs=get;list;watch
// +kubebuilder:rbac:groups=core,resources=configmaps,verbs=get;list;watch;patch
// +kubebuilder:rbac:groups=core,resources=secrets,verbs=get;list;watch;patch

func (r *HRAutodicoverReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	log := log.FromContext(ctx)
	log.Info("Reconciling HelmRelease for autodiscovery", "name", req.NamespacedName)
	var hr helmv2.HelmRelease
	if err := r.Get(ctx, req.NamespacedName, &hr); err != nil {
		return ctrl.Result{}, client.IgnoreNotFound(err)
	}

	if hr.Spec.ValuesFrom != nil {
		for _, valueFrom := range hr.Spec.ValuesFrom {
			if err := r.processValueFrom(ctx, &hr, valueFrom); err != nil {
				if client.IgnoreNotFound(err) == nil {
					log.Info("Resource not found, skipping", "kind", valueFrom.Kind, "name", valueFrom.Name, "namespace", hr.Namespace)
					continue
				}
				log.Error(err, "Failed to process valuesFrom", "kind", valueFrom.Kind, "name", valueFrom.Name, "namespace", hr.Namespace)
				return ctrl.Result{}, err
			}
		}
	}

	return ctrl.Result{}, nil
}

func (r *HRAutodicoverReconciler) processValueFrom(ctx context.Context, hr *helmv2.HelmRelease, valueFrom helmv2.ValuesReference) error {
	namespace := hr.Namespace

	annotations := map[string]string{
		HRNameAnnotation: hr.Name,
		HRNSAnnotation:   namespace,
	}

	labels := map[string]string{
		LabelReconcilerNameSourceKey: "true",
	}

	obj := &corev1.ObjectReference{
		Kind:      valueFrom.Kind,
		Name:      valueFrom.Name,
		Namespace: namespace,
	}

	return r.addAnnotationsAndLabel(ctx, obj, annotations, labels)
}

func (r *HRAutodicoverReconciler) addAnnotationsAndLabel(
	ctx context.Context, obj *corev1.ObjectReference, annotations, labels map[string]string) error {

	var resource client.Object

	switch obj.Kind {
	case "ConfigMap":
		resource = &corev1.ConfigMap{}
	case "Secret":
		resource = &corev1.Secret{}
	default:
		return fmt.Errorf("unsupported resource kind: %s", obj.Kind)
	}

	if err := r.Get(ctx, types.NamespacedName{
		Name:      obj.Name,
		Namespace: obj.Namespace,
	}, resource); err != nil {
		return err
	}

	if resource.GetAnnotations() == nil {
		resource.SetAnnotations(make(map[string]string))
	}
	if resource.GetLabels() == nil {
		resource.SetLabels(make(map[string]string))
	}

	updatedAnnotations := false
	for key, value := range annotations {
		if resource.GetAnnotations()[key] != value {
			resource.GetAnnotations()[key] = value
			updatedAnnotations = true
		}
	}

	updatedLabels := false
	for key, value := range labels {
		if resource.GetLabels()[key] != value {
			resource.GetLabels()[key] = value
			updatedLabels = true
		}
	}

	if updatedAnnotations && updatedLabels {
		if err := r.Update(ctx, resource); err != nil {
			return err
		}
	}

	return nil
}

func (r *HRAutodicoverReconciler) SetupWithManager(mgr ctrl.Manager,
	maxConcurrency int, cacheSyncTimeout time.Duration) error {
	return ctrl.NewControllerManagedBy(mgr).Named(HRAutodicoverReconcilerName).
		For(&helmv2.HelmRelease{}, builder.WithPredicates(predicate.Funcs{
			CreateFunc: func(e event.CreateEvent) bool {
				return true
			},
			DeleteFunc: func(e event.DeleteEvent) bool {
				return false
			},
			UpdateFunc: func(e event.UpdateEvent) bool {
				return true
			},
			GenericFunc: func(e event.GenericEvent) bool {
				return false
			},
		})).
		WithOptions(ControllerOptions(maxConcurrency, cacheSyncTimeout)).
		Complete(r)
}

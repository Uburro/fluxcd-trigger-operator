package controller

import (
	"context"
	"fmt"
	"hash/adler32"
	"sync"
	"time"

	helmv2 "github.com/fluxcd/helm-controller/api/v2"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/util/workqueue"
	"k8s.io/kubernetes/pkg/util/hash"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller"
	"sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"
)

const (
	LabelReconcilerNameSourceKey = "uburro.github.com/fluxcd-trigger-operator"
	HRNameAnnotation             = "uburro.github.com/helmreleases-name"
	HRNSAnnotation               = "uburro.github.com/helmreleases-namespace"
	HashAnnotation               = "uburro.github.com/config-digest"
	DefaultFluxcdNamespace       = "flux-system"
)

var (
	optionsInit    sync.Once
	defaultOptions *controller.Options
)

func ControllerOptions(maxConcurrency int, cacheSyncTimeout time.Duration) controller.Options {
	rateLimiters := workqueue.NewTypedItemExponentialFailureRateLimiter[reconcile.Request](30*time.Second, 5*time.Minute)
	optionsInit.Do(func() {
		defaultOptions = &controller.Options{
			RateLimiter:             rateLimiters,
			CacheSyncTimeout:        cacheSyncTimeout,
			MaxConcurrentReconciles: maxConcurrency,
		}
	})
	return *defaultOptions
}

func getDataHashCM(data map[string]string) string {
	hasher := adler32.New()
	hash.DeepHashObject(hasher, data)
	return fmt.Sprintf("%x", hasher.Sum32())
}

func getDataHashSecret(data map[string][]byte) string {
	hasher := adler32.New()
	hash.DeepHashObject(hasher, data)
	return fmt.Sprintf("%x", hasher.Sum32())
}

type BaseReconciler struct {
	client.Client
	Scheme *runtime.Scheme
}

type ResourceMetadata interface {
	GetAnnotations() map[string]string
	GetLabels() map[string]string
}

func (r *BaseReconciler) ReconcileResource(
	ctx context.Context,
	req ctrl.Request,
	resource client.Object,
) (ctrl.Result, error) {
	logger := log.FromContext(ctx)
	resourceType := fmt.Sprintf("%T", resource)
	logger.Info(fmt.Sprintf("reconciling %s", resourceType), "name", req.Name, "namespace", req.Namespace)

	if err := r.Get(ctx, req.NamespacedName, resource); err != nil {
		logger.V(1).Error(err, fmt.Sprintf("Getting %s produced an error", resourceType), "resource", req.Name)
		return ctrl.Result{}, err
	}

	hrName, hrNamespace, oldDigest := r.ExtractFromAnnotations(resource)

	if hrName == "" {
		logger.V(1).Info(fmt.Sprintf("No HelmRelease name found in %s annotations", resourceType), "resource", req.Name)
		return ctrl.Result{}, nil
	}

	hr, err := r.GetHelmRelease(ctx, hrName, hrNamespace)
	if err != nil {
		logger.V(1).Error(err, "Getting HelmRelease produced an error",
			"helmRelease", hrName, "namespace", hrNamespace)
		return ctrl.Result{}, err
	}

	if !r.IsDeployed(hr) {
		logger.V(1).Info("HelmRelease is not deployed, skipping patch", "helmRelease",
			hrName, "namespace", hrNamespace)
		return ctrl.Result{}, nil
	}

	newDigest := r.getNewDigest(resource)

	logger.V(1).Info("old digest", "digest", oldDigest)
	logger.V(1).Info("new digest", "digest", newDigest)

	if oldDigest == newDigest {
		logger.V(1).Info(fmt.Sprintf("No changes detected in %s, skipping HelmRelease patch", resourceType), "resource", req.Name)
		return ctrl.Result{}, nil
	}

	if err := r.PatchHR(ctx, hr); err != nil {
		logger.Error(err, "failed to patch HelmRelease", "name", hr.Name)
		return ctrl.Result{}, err
	}

	if err := r.UpdateDigest(ctx, resource, newDigest); err != nil {
		logger.Error(err, fmt.Sprintf("failed to patch %s", resourceType), "name", resource.GetName())
		return ctrl.Result{}, err
	}

	logger.Info("patched HelmRelease with new digest",
		"name", hr.Name,
		"digest", newDigest,
		"version", hr.Status.History[0].Version)

	return ctrl.Result{}, nil
}

func (r *BaseReconciler) ExtractFromAnnotations(resource ResourceMetadata) (string, string, string) {
	annotations := resource.GetAnnotations()
	hrName := annotations[HRNameAnnotation]
	hrNamespace := annotations[HRNSAnnotation]
	if hrNamespace == "" {
		hrNamespace = DefaultFluxcdNamespace
	}
	oldDigest := annotations[HashAnnotation]
	return hrName, hrNamespace, oldDigest
}

func (r *BaseReconciler) GetHelmRelease(ctx context.Context, name, namespace string) (*helmv2.HelmRelease, error) {
	hr := &helmv2.HelmRelease{}
	err := r.Get(ctx, client.ObjectKey{
		Name:      name,
		Namespace: namespace,
	}, hr)
	return hr, err
}

func (r *BaseReconciler) IsDeployed(hr *helmv2.HelmRelease) bool {
	return len(hr.Status.History) > 0 && hr.Status.History[0].Status == "deployed"
}

func (r *BaseReconciler) getNewDigest(resource client.Object) string {
	switch obj := resource.(type) {
	case *corev1.Secret:
		return getDataHashSecret(obj.Data)
	case *corev1.ConfigMap:
		return getDataHashCM(obj.Data)
	default:
		return ""
	}
}

func (r *BaseReconciler) GetCurrentDigest(hr *helmv2.HelmRelease) string {
	if len(hr.Status.History) == 0 {
		return ""
	}
	return hr.Status.History[0].Digest
}

func (r *BaseReconciler) PatchHR(ctx context.Context, hr *helmv2.HelmRelease) error {
	patchTarget := hr.DeepCopy()
	ts := time.Now().Format(time.RFC3339Nano)

	if patchTarget.Annotations == nil {
		patchTarget.Annotations = make(map[string]string)
	}
	patchTarget.Annotations["reconcile.fluxcd.io/forceAt"] = ts
	patchTarget.Annotations["reconcile.fluxcd.io/requestedAt"] = ts

	patch := client.MergeFrom(hr.DeepCopy())
	return r.Patch(ctx, patchTarget, patch)
}

func (r *BaseReconciler) UpdateDigest(ctx context.Context, obj client.Object, newDigest string) error {
	patchTarget := obj.DeepCopyObject().(client.Object)

	annotations := patchTarget.GetAnnotations()
	if annotations == nil {
		annotations = make(map[string]string)
	}
	annotations[HashAnnotation] = newDigest
	patchTarget.SetAnnotations(annotations)

	patch := client.MergeFrom(obj.DeepCopyObject().(client.Object))
	return r.Patch(ctx, patchTarget, patch)
}

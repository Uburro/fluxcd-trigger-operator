package controller_test

import (
	"context"
	"time"

	helmv2 "github.com/fluxcd/helm-controller/api/v2"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"

	"github.com/uburro/fluxcd-trigger-operator/internal/controller"
)

var _ = Describe("Controller", func() {
	var (
		reconciler      *controller.BaseReconciler
		fakeClient      client.Client
		ctx             context.Context
		scheme          *runtime.Scheme
		testNamespace   = "test-namespace"
		fluxNamespace   = "flux-system"
		testConfigMap   *corev1.ConfigMap
		testSecret      *corev1.Secret
		testHelmRelease *helmv2.HelmRelease
	)

	BeforeEach(func() {
		ctx = context.Background()
		scheme = runtime.NewScheme()
		Expect(corev1.AddToScheme(scheme)).To(Succeed())
		Expect(helmv2.AddToScheme(scheme)).To(Succeed())

		fakeClient = fake.NewClientBuilder().WithScheme(scheme).Build()
		reconciler = &controller.BaseReconciler{
			Client: fakeClient,
			Scheme: scheme,
		}

		// Создаем тестовый HelmRelease
		testHelmRelease = &helmv2.HelmRelease{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "test-helm-release",
				Namespace: fluxNamespace,
			},
			Status: helmv2.HelmReleaseStatus{
				History: helmv2.Snapshots{
					{
						Version: 1,
						Status:  "deployed",
						Digest:  "old-digest",
					},
				},
			},
		}

		// Создаем тестовый ConfigMap
		testConfigMap = &corev1.ConfigMap{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "test-configmap",
				Namespace: testNamespace,
				Annotations: map[string]string{
					controller.HRAnnotation:   "test-helm-release",
					controller.NSAnnotation:   fluxNamespace,
					controller.HashAnnotation: "old-hash",
				},
			},
			Data: map[string]string{
				"key1": "value1",
				"key2": "value2",
			},
		}

		// Создаем тестовый Secret
		testSecret = &corev1.Secret{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "test-secret",
				Namespace: testNamespace,
				Annotations: map[string]string{
					controller.HRAnnotation:   "test-helm-release",
					controller.NSAnnotation:   fluxNamespace,
					controller.HashAnnotation: "old-hash",
				},
			},
			Data: map[string][]byte{
				"username": []byte("admin"),
				"password": []byte("secret"),
			},
		}
	})

	Describe("ControllerOptions", func() {
		It("should return controller options with correct configuration", func() {
			maxConcurrency := 5
			cacheSyncTimeout := 30 * time.Second

			options := controller.ControllerOptions(maxConcurrency, cacheSyncTimeout)

			Expect(options.MaxConcurrentReconciles).To(Equal(maxConcurrency))
			Expect(options.CacheSyncTimeout).To(Equal(cacheSyncTimeout))
			Expect(options.RateLimiter).NotTo(BeNil())
		})

		It("should return the same options on subsequent calls (singleton pattern)", func() {
			options1 := controller.ControllerOptions(5, 30*time.Second)
			options2 := controller.ControllerOptions(10, 60*time.Second)

			// Проверяем, что вернулись те же опции (singleton)
			Expect(options1.MaxConcurrentReconciles).To(Equal(options2.MaxConcurrentReconciles))
			Expect(options1.CacheSyncTimeout).To(Equal(options2.CacheSyncTimeout))
		})
	})

	Describe("ExtractFromAnnotations", func() {
		It("should extract HelmRelease name, namespace and digest from annotations", func() {
			hrName, hrNamespace, digest := reconciler.ExtractFromAnnotations(testConfigMap)

			Expect(hrName).To(Equal("test-helm-release"))
			Expect(hrNamespace).To(Equal(fluxNamespace))
			Expect(digest).To(Equal("old-hash"))
		})

		It("should use default namespace when namespace annotation is missing", func() {
			testConfigMap.Annotations[controller.NSAnnotation] = ""

			hrName, hrNamespace, digest := reconciler.ExtractFromAnnotations(testConfigMap)

			Expect(hrName).To(Equal("test-helm-release"))
			Expect(hrNamespace).To(Equal(controller.DefaultFluxcdNamespace))
			Expect(digest).To(Equal("old-hash"))
		})

		It("should handle missing annotations gracefully", func() {
			testConfigMap.Annotations = nil

			hrName, hrNamespace, digest := reconciler.ExtractFromAnnotations(testConfigMap)

			Expect(hrName).To(BeEmpty())
			Expect(hrNamespace).To(Equal(controller.DefaultFluxcdNamespace))
			Expect(digest).To(BeEmpty())
		})
	})

	Describe("GetHelmRelease", func() {
		BeforeEach(func() {
			Expect(fakeClient.Create(ctx, testHelmRelease)).To(Succeed())
		})

		It("should retrieve HelmRelease successfully", func() {
			hr, err := reconciler.GetHelmRelease(ctx, "test-helm-release", fluxNamespace)

			Expect(err).NotTo(HaveOccurred())
			Expect(hr.Name).To(Equal("test-helm-release"))
			Expect(hr.Namespace).To(Equal(fluxNamespace))
		})

		It("should return error when HelmRelease does not exist", func() {
			_, err := reconciler.GetHelmRelease(ctx, "non-existent", fluxNamespace)

			Expect(err).To(HaveOccurred())
		})
	})

	Describe("IsDeployed", func() {
		It("should return true when HelmRelease is deployed", func() {
			isDeployed := reconciler.IsDeployed(testHelmRelease)
			Expect(isDeployed).To(BeTrue())
		})

		It("should return false when HelmRelease has no history", func() {
			testHelmRelease.Status.History = helmv2.Snapshots([]*helmv2.Snapshot{})

			isDeployed := reconciler.IsDeployed(testHelmRelease)
			Expect(isDeployed).To(BeFalse())
		})

		It("should return false when HelmRelease status is not deployed", func() {
			testHelmRelease.Status.History[0].Status = "failed"

			isDeployed := reconciler.IsDeployed(testHelmRelease)
			Expect(isDeployed).To(BeFalse())
		})
	})

	Describe("PatchHR", func() {
		BeforeEach(func() {
			Expect(fakeClient.Create(ctx, testHelmRelease)).To(Succeed())
		})

		It("should patch HelmRelease with force reconciliation annotations", func() {
			err := reconciler.PatchHR(ctx, testHelmRelease)
			Expect(err).NotTo(HaveOccurred())

			// Получаем обновленный HelmRelease
			updatedHR := &helmv2.HelmRelease{}
			err = fakeClient.Get(ctx, types.NamespacedName{
				Name:      testHelmRelease.Name,
				Namespace: testHelmRelease.Namespace,
			}, updatedHR)
			Expect(err).NotTo(HaveOccurred())

			// Проверяем, что аннотации были добавлены
			Expect(updatedHR.Annotations).To(HaveKey("reconcile.fluxcd.io/forceAt"))
			Expect(updatedHR.Annotations).To(HaveKey("reconcile.fluxcd.io/requestedAt"))
		})
	})

	Describe("UpdateDigest", func() {
		BeforeEach(func() {
			Expect(fakeClient.Create(ctx, testConfigMap)).To(Succeed())
		})

		It("should update digest annotation on resource", func() {
			newDigest := "new-digest-value"

			err := reconciler.UpdateDigest(ctx, testConfigMap, newDigest)
			Expect(err).NotTo(HaveOccurred())

			// Получаем обновленный ConfigMap
			updatedCM := &corev1.ConfigMap{}
			err = fakeClient.Get(ctx, types.NamespacedName{
				Name:      testConfigMap.Name,
				Namespace: testConfigMap.Namespace,
			}, updatedCM)
			Expect(err).NotTo(HaveOccurred())

			Expect(updatedCM.Annotations[controller.HashAnnotation]).To(Equal(newDigest))
		})

		It("should create annotations map if it doesn't exist", func() {
			testConfigMap.Annotations = nil
			Expect(fakeClient.Update(ctx, testConfigMap)).To(Succeed())

			newDigest := "new-digest-value"
			err := reconciler.UpdateDigest(ctx, testConfigMap, newDigest)
			Expect(err).NotTo(HaveOccurred())

			// Получаем обновленный ConfigMap
			updatedCM := &corev1.ConfigMap{}
			err = fakeClient.Get(ctx, types.NamespacedName{
				Name:      testConfigMap.Name,
				Namespace: testConfigMap.Namespace,
			}, updatedCM)
			Expect(err).NotTo(HaveOccurred())

			Expect(updatedCM.Annotations).NotTo(BeNil())
			Expect(updatedCM.Annotations[controller.HashAnnotation]).To(Equal(newDigest))
		})
	})

	Describe("ReconcileResource", func() {
		Context("when reconciling ConfigMap", func() {
			BeforeEach(func() {
				Expect(fakeClient.Create(ctx, testConfigMap)).To(Succeed())
				Expect(fakeClient.Create(ctx, testHelmRelease)).To(Succeed())
			})

			It("should successfully reconcile when data changes", func() {
				// Изменяем данные ConfigMap чтобы создать новый хеш
				testConfigMap.Data["key3"] = "value3"
				Expect(fakeClient.Update(ctx, testConfigMap)).To(Succeed())

				req := ctrl.Request{
					NamespacedName: types.NamespacedName{
						Name:      testConfigMap.Name,
						Namespace: testConfigMap.Namespace,
					},
				}

				result, err := reconciler.ReconcileResource(ctx, req, &corev1.ConfigMap{})

				Expect(err).NotTo(HaveOccurred())
				Expect(result).To(Equal(ctrl.Result{}))
			})

			It("should skip reconciliation when HelmRelease annotation is missing", func() {
				// Удаляем аннотацию HelmRelease
				delete(testConfigMap.Annotations, controller.HRAnnotation)
				Expect(fakeClient.Update(ctx, testConfigMap)).To(Succeed())

				req := ctrl.Request{
					NamespacedName: types.NamespacedName{
						Name:      testConfigMap.Name,
						Namespace: testConfigMap.Namespace,
					},
				}

				result, err := reconciler.ReconcileResource(ctx, req, &corev1.ConfigMap{})

				Expect(err).NotTo(HaveOccurred())
				Expect(result).To(Equal(ctrl.Result{}))
			})

			It("should skip reconciliation when HelmRelease is not deployed", func() {
				// Меняем статус HelmRelease на "failed"
				testHelmRelease.Status.History[0].Status = "failed"
				Expect(fakeClient.Update(ctx, testHelmRelease)).To(Succeed())

				req := ctrl.Request{
					NamespacedName: types.NamespacedName{
						Name:      testConfigMap.Name,
						Namespace: testConfigMap.Namespace,
					},
				}

				result, err := reconciler.ReconcileResource(ctx, req, &corev1.ConfigMap{})

				Expect(err).NotTo(HaveOccurred())
				Expect(result).To(Equal(ctrl.Result{}))
			})

			It("should skip reconciliation when digest hasn't changed", func() {
				req := ctrl.Request{
					NamespacedName: types.NamespacedName{
						Name:      testConfigMap.Name,
						Namespace: testConfigMap.Namespace,
					},
				}

				// Первый запуск для установки правильного хеша
				_, err := reconciler.ReconcileResource(ctx, req, &corev1.ConfigMap{})
				Expect(err).NotTo(HaveOccurred())

				// Второй запуск должен пропустить reconciliation
				result, err := reconciler.ReconcileResource(ctx, req, &corev1.ConfigMap{})

				Expect(err).NotTo(HaveOccurred())
				Expect(result).To(Equal(ctrl.Result{}))
			})
		})

		Context("when reconciling Secret", func() {
			BeforeEach(func() {
				Expect(fakeClient.Create(ctx, testSecret)).To(Succeed())
				Expect(fakeClient.Create(ctx, testHelmRelease)).To(Succeed())
			})

			It("should successfully reconcile Secret when data changes", func() {
				// Изменяем данные Secret
				testSecret.Data["newkey"] = []byte("newvalue")
				Expect(fakeClient.Update(ctx, testSecret)).To(Succeed())

				req := ctrl.Request{
					NamespacedName: types.NamespacedName{
						Name:      testSecret.Name,
						Namespace: testSecret.Namespace,
					},
				}

				result, err := reconciler.ReconcileResource(ctx, req, &corev1.Secret{})

				Expect(err).NotTo(HaveOccurred())
				Expect(result).To(Equal(ctrl.Result{}))
			})
		})

		Context("when resource doesn't exist", func() {
			It("should return error when trying to reconcile non-existent resource", func() {
				req := ctrl.Request{
					NamespacedName: types.NamespacedName{
						Name:      "non-existent",
						Namespace: testNamespace,
					},
				}

				_, err := reconciler.ReconcileResource(ctx, req, &corev1.ConfigMap{})

				Expect(err).To(HaveOccurred())
			})
		})

		Context("when HelmRelease doesn't exist", func() {
			BeforeEach(func() {
				Expect(fakeClient.Create(ctx, testConfigMap)).To(Succeed())
			})

			It("should return error when HelmRelease is not found", func() {
				req := ctrl.Request{
					NamespacedName: types.NamespacedName{
						Name:      testConfigMap.Name,
						Namespace: testConfigMap.Namespace,
					},
				}

				_, err := reconciler.ReconcileResource(ctx, req, &corev1.ConfigMap{})

				Expect(err).To(HaveOccurred())
			})
		})
	})

	Describe("GetCurrentDigest", func() {
		It("should return digest from HelmRelease history", func() {
			digest := reconciler.GetCurrentDigest(testHelmRelease)
			Expect(digest).To(Equal("old-digest"))
		})

		It("should return empty string when no history exists", func() {
			testHelmRelease.Status.History = helmv2.Snapshots([]*helmv2.Snapshot{})

			digest := reconciler.GetCurrentDigest(testHelmRelease)
			Expect(digest).To(BeEmpty())
		})
	})
})

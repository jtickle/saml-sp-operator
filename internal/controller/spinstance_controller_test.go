/*
Copyright 2026.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package controller

import (
	"context"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	apimeta "k8s.io/apimachinery/pkg/api/meta"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	samlv1alpha1 "github.com/jtickle/saml-sp-operator/api/v1alpha1"
)

const spImageTest = "ghcr.io/jtickle/saml-sp-operator/shib-authenticator@sha256:0e33ee7fea4524cb3caa8744b22f05a80703d22444ef198368484dc523f41319"

var _ = Describe("SPInstance Controller", func() {
	Context("When reconciling a resource", func() {
		const (
			resourceName      = "test-resource"
			resourceNamespace = "default"
		)

		ctx := context.Background()

		typeNamespacedName := types.NamespacedName{
			Name:      resourceName,
			Namespace: resourceNamespace,
		}
		spinstance := &samlv1alpha1.SPInstance{}

		BeforeEach(func() {
			By("creating the credentials Secret referenced by the SPInstance")
			secret := &corev1.Secret{}
			secretName := types.NamespacedName{Name: "sp-keypair", Namespace: resourceNamespace}
			secretErr := k8sClient.Get(ctx, secretName, secret)
			if secretErr != nil && errors.IsNotFound(secretErr) {
				secret = &corev1.Secret{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "sp-keypair",
						Namespace: resourceNamespace,
					},
					StringData: map[string]string{
						"tls.crt": "dummy-cert",
						"tls.key": "dummy-key",
					},
				}
				Expect(k8sClient.Create(ctx, secret)).To(Succeed())
			}

			By("creating the custom resource for the Kind SPInstance")
			err := k8sClient.Get(ctx, typeNamespacedName, spinstance)
			if err != nil && errors.IsNotFound(err) {
				resource := &samlv1alpha1.SPInstance{
					ObjectMeta: metav1.ObjectMeta{
						Name:      resourceName,
						Namespace: resourceNamespace,
					},
					Spec: samlv1alpha1.SPInstanceSpec{
						EntityID:    "https://sp.example.com/shibboleth",
						Credentials: samlv1alpha1.SecretReference{Name: "sp-keypair"},
						IdP: samlv1alpha1.IdPConfig{
							MetadataURL: "https://mocksaml.com/api/saml/metadata",
						},
					},
				}
				Expect(k8sClient.Create(ctx, resource)).To(Succeed())
			}
		})

		AfterEach(func() {
			// TODO(user): Cleanup logic after each test, like removing the resource instance.
			resource := &samlv1alpha1.SPInstance{}
			err := k8sClient.Get(ctx, typeNamespacedName, resource)
			Expect(err).NotTo(HaveOccurred())

			By("Cleanup the specific resource instance SPInstance")
			Expect(k8sClient.Delete(ctx, resource)).To(Succeed())

			By("Cleanup the credentials Secret")
			secret := &corev1.Secret{}
			secretName := types.NamespacedName{Name: "sp-keypair", Namespace: resourceNamespace}
			if err := k8sClient.Get(ctx, secretName, secret); err == nil {
				Expect(k8sClient.Delete(ctx, secret)).To(Succeed())
			}
		})
		It("should successfully reconcile the resource", func() {
			By("Reconciling the created resource")
			controllerReconciler := &SPInstanceReconciler{
				Client:  k8sClient,
				Scheme:  k8sClient.Scheme(),
				SPImage: spImageTest,
			}

			_, err := controllerReconciler.Reconcile(ctx, reconcile.Request{
				NamespacedName: typeNamespacedName,
			})
			Expect(err).NotTo(HaveOccurred())
			// TODO(user): Add more specific assertions depending on your controller's reconciliation logic.
			// Example: If you expect a certain status condition after reconciliation, verify it here.
		})

		It("should reconcile a ConfigMap holding the rendered SP config, owned by the SPInstance", func() {
			By("Reconciling the created resource")
			controllerReconciler := &SPInstanceReconciler{
				Client:  k8sClient,
				Scheme:  k8sClient.Scheme(),
				SPImage: spImageTest,
			}

			_, err := controllerReconciler.Reconcile(ctx, reconcile.Request{
				NamespacedName: typeNamespacedName,
			})
			Expect(err).NotTo(HaveOccurred())

			By("checking the ConfigMap was created with the rendered config keys")
			cm := &corev1.ConfigMap{}
			cmName := types.NamespacedName{Name: resourceName + "-sp", Namespace: resourceNamespace}
			Expect(k8sClient.Get(ctx, cmName, cm)).To(Succeed())

			for _, key := range []string{"shibboleth2.xml", "attribute-map.xml", "nginx.conf"} {
				Expect(cm.Data).To(HaveKey(key))
				Expect(cm.Data[key]).NotTo(BeEmpty())
			}

			By("checking the ConfigMap has an ownerRef to the SPInstance")
			Expect(k8sClient.Get(ctx, typeNamespacedName, spinstance)).To(Succeed())
			Expect(cm.OwnerReferences).To(HaveLen(1))
			owner := cm.OwnerReferences[0]
			Expect(owner.Kind).To(Equal("SPInstance"))
			Expect(owner.Name).To(Equal(resourceName))
			Expect(owner.UID).To(Equal(spinstance.UID))
			Expect(owner.Controller).NotTo(BeNil())
			Expect(*owner.Controller).To(BeTrue())
		})

		It("should reconcile a Deployment with rollout gating, fail-safe, and readiness probe", func() {
			By("Reconciling the created resource")
			controllerReconciler := &SPInstanceReconciler{
				Client:  k8sClient,
				Scheme:  k8sClient.Scheme(),
				SPImage: spImageTest,
			}

			_, err := controllerReconciler.Reconcile(ctx, reconcile.Request{
				NamespacedName: typeNamespacedName,
			})
			Expect(err).NotTo(HaveOccurred())

			By("checking the Deployment was created with the expected spec")
			dep := &appsv1.Deployment{}
			depName := types.NamespacedName{Name: resourceName + "-sp", Namespace: resourceNamespace}
			Expect(k8sClient.Get(ctx, depName, dep)).To(Succeed())

			By("checking the fail-safe rollout strategy (SPI-07)")
			Expect(dep.Spec.Strategy.Type).To(Equal(appsv1.RollingUpdateDeploymentStrategyType))
			Expect(dep.Spec.Strategy.RollingUpdate).NotTo(BeNil())
			Expect(dep.Spec.Strategy.RollingUpdate.MaxUnavailable).NotTo(BeNil())
			Expect(dep.Spec.Strategy.RollingUpdate.MaxUnavailable.IntValue()).To(Equal(0))

			By("checking the pod-template config-hash annotation (SPI-02)")
			firstHash := dep.Spec.Template.Annotations["saml.tickletechnologies.com/config-hash"]
			Expect(firstHash).NotTo(BeEmpty())

			By("checking the container spec")
			Expect(dep.Spec.Template.Spec.Containers).To(HaveLen(1))
			c := dep.Spec.Template.Spec.Containers[0]
			Expect(c.Image).To(Equal(spImageTest))
			Expect(c.Env).To(ContainElement(corev1.EnvVar{Name: "SHIBSP_SERVER_SCHEME", Value: "https"}))
			Expect(c.Ports).To(ContainElement(HaveField("ContainerPort", int32(8080))))

			By("checking the real shibd readiness probe (SPI-03)")
			Expect(c.ReadinessProbe).NotTo(BeNil())
			Expect(c.ReadinessProbe.Exec).NotTo(BeNil())
			Expect(c.ReadinessProbe.Exec.Command).To(Equal([]string{
				"curl", "-fsS", "http://localhost:8080/Shibboleth.sso/Status",
			}))

			By("checking the ConfigMap, credential Secret, and emptyDir mounts")
			Expect(c.VolumeMounts).To(ContainElement(corev1.VolumeMount{
				Name: "shib-config", MountPath: "/etc/shibboleth/shibboleth2.xml", SubPath: "shibboleth2.xml",
			}))
			Expect(c.VolumeMounts).To(ContainElement(corev1.VolumeMount{
				Name: "shib-config", MountPath: "/etc/shibboleth/attribute-map.xml", SubPath: "attribute-map.xml",
			}))
			Expect(c.VolumeMounts).To(ContainElement(corev1.VolumeMount{
				Name: "shib-config", MountPath: "/etc/nginx/nginx.conf", SubPath: "nginx.conf",
			}))
			Expect(c.VolumeMounts).To(ContainElement(corev1.VolumeMount{
				Name: "sp-credentials", MountPath: "/run/shibboleth/sp-credentials", ReadOnly: true,
			}))
			Expect(c.VolumeMounts).To(ContainElement(corev1.VolumeMount{
				Name: "shib-run", MountPath: "/run/shibboleth",
			}))

			var shibConfigVol, spCredentialsVol, shibRunVol *corev1.Volume
			for i := range dep.Spec.Template.Spec.Volumes {
				v := &dep.Spec.Template.Spec.Volumes[i]
				switch v.Name {
				case "shib-config":
					shibConfigVol = v
				case "sp-credentials":
					spCredentialsVol = v
				case "shib-run":
					shibRunVol = v
				}
			}
			Expect(shibConfigVol).NotTo(BeNil())
			Expect(shibConfigVol.ConfigMap).NotTo(BeNil())
			Expect(shibConfigVol.ConfigMap.Name).To(Equal(resourceName + "-sp"))

			Expect(spCredentialsVol).NotTo(BeNil())
			Expect(spCredentialsVol.Secret).NotTo(BeNil())
			Expect(spCredentialsVol.Secret.SecretName).To(Equal(spinstance.Spec.Credentials.Name))

			Expect(shibRunVol).NotTo(BeNil())
			Expect(shibRunVol.EmptyDir).NotTo(BeNil())

			By("checking the Deployment has an ownerRef to the SPInstance")
			Expect(k8sClient.Get(ctx, typeNamespacedName, spinstance)).To(Succeed())
			Expect(dep.OwnerReferences).To(HaveLen(1))
			owner := dep.OwnerReferences[0]
			Expect(owner.Kind).To(Equal("SPInstance"))
			Expect(owner.Name).To(Equal(resourceName))
			Expect(owner.UID).To(Equal(spinstance.UID))
			Expect(owner.Controller).NotTo(BeNil())
			Expect(*owner.Controller).To(BeTrue())

			By("reconciling again with the SAME spec: the config-hash annotation must be unchanged (SPI-02)")
			_, err = controllerReconciler.Reconcile(ctx, reconcile.Request{
				NamespacedName: typeNamespacedName,
			})
			Expect(err).NotTo(HaveOccurred())

			depAfterNoop := &appsv1.Deployment{}
			Expect(k8sClient.Get(ctx, depName, depAfterNoop)).To(Succeed())
			Expect(depAfterNoop.Spec.Template.Annotations["saml.tickletechnologies.com/config-hash"]).To(Equal(firstHash))

			By("mutating entityID: the config-hash annotation must change")
			Expect(k8sClient.Get(ctx, typeNamespacedName, spinstance)).To(Succeed())
			spinstance.Spec.EntityID = "https://sp.example.com/shibboleth-changed"
			Expect(k8sClient.Update(ctx, spinstance)).To(Succeed())

			_, err = controllerReconciler.Reconcile(ctx, reconcile.Request{
				NamespacedName: typeNamespacedName,
			})
			Expect(err).NotTo(HaveOccurred())

			depAfterChange := &appsv1.Deployment{}
			Expect(k8sClient.Get(ctx, depName, depAfterChange)).To(Succeed())
			Expect(depAfterChange.Spec.Template.Annotations["saml.tickletechnologies.com/config-hash"]).NotTo(Equal(firstHash))
		})

		It("should reconcile a ClusterIP Service and a headless Service, both selecting the SP pod labels", func() {
			By("Reconciling the created resource")
			controllerReconciler := &SPInstanceReconciler{
				Client:  k8sClient,
				Scheme:  k8sClient.Scheme(),
				SPImage: spImageTest,
			}

			_, err := controllerReconciler.Reconcile(ctx, reconcile.Request{
				NamespacedName: typeNamespacedName,
			})
			Expect(err).NotTo(HaveOccurred())

			By("checking the Deployment's pod-template labels, which the Services must select")
			dep := &appsv1.Deployment{}
			depName := types.NamespacedName{Name: resourceName + "-sp", Namespace: resourceNamespace}
			Expect(k8sClient.Get(ctx, depName, dep)).To(Succeed())
			podLabels := dep.Spec.Template.Labels
			Expect(podLabels).To(HaveKeyWithValue("app.kubernetes.io/name", "sp"))
			Expect(podLabels).To(HaveKeyWithValue("app.kubernetes.io/instance", resourceName))

			Expect(k8sClient.Get(ctx, typeNamespacedName, spinstance)).To(Succeed())

			By("checking the ClusterIP Service")
			svc := &corev1.Service{}
			svcName := types.NamespacedName{Name: resourceName + "-sp", Namespace: resourceNamespace}
			Expect(k8sClient.Get(ctx, svcName, svc)).To(Succeed())
			Expect(svc.Spec.Selector).To(Equal(podLabels))
			Expect(svc.Spec.Ports).To(ContainElement(HaveField("Port", int32(8080))))
			Expect(svc.Spec.ClusterIP).NotTo(Equal("None"))

			Expect(svc.OwnerReferences).To(HaveLen(1))
			svcOwner := svc.OwnerReferences[0]
			Expect(svcOwner.Kind).To(Equal("SPInstance"))
			Expect(svcOwner.Name).To(Equal(resourceName))
			Expect(svcOwner.UID).To(Equal(spinstance.UID))
			Expect(svcOwner.Controller).NotTo(BeNil())
			Expect(*svcOwner.Controller).To(BeTrue())

			By("checking the headless Service")
			headlessSvc := &corev1.Service{}
			headlessSvcName := types.NamespacedName{Name: resourceName + "-sp-headless", Namespace: resourceNamespace}
			Expect(k8sClient.Get(ctx, headlessSvcName, headlessSvc)).To(Succeed())
			Expect(headlessSvc.Spec.Selector).To(Equal(podLabels))
			Expect(headlessSvc.Spec.Ports).To(ContainElement(HaveField("Port", int32(8080))))
			Expect(headlessSvc.Spec.ClusterIP).To(Equal("None"))

			Expect(headlessSvc.OwnerReferences).To(HaveLen(1))
			headlessOwner := headlessSvc.OwnerReferences[0]
			Expect(headlessOwner.Kind).To(Equal("SPInstance"))
			Expect(headlessOwner.Name).To(Equal(resourceName))
			Expect(headlessOwner.UID).To(Equal(spinstance.UID))
			Expect(headlessOwner.Controller).NotTo(BeNil())
			Expect(*headlessOwner.Controller).To(BeTrue())
		})

		It("should set configHash, observedGeneration, ConfigRendered, and Ready reflecting Deployment availability", func() {
			By("Reconciling the created resource")
			controllerReconciler := &SPInstanceReconciler{
				Client:  k8sClient,
				Scheme:  k8sClient.Scheme(),
				SPImage: spImageTest,
			}

			_, err := controllerReconciler.Reconcile(ctx, reconcile.Request{
				NamespacedName: typeNamespacedName,
			})
			Expect(err).NotTo(HaveOccurred())

			By("checking status.configHash, observedGeneration, and ConfigRendered")
			Expect(k8sClient.Get(ctx, typeNamespacedName, spinstance)).To(Succeed())
			Expect(spinstance.Status.ConfigHash).NotTo(BeEmpty())
			Expect(spinstance.Status.ObservedGeneration).To(Equal(spinstance.Generation))

			configRendered := apimeta.FindStatusCondition(spinstance.Status.Conditions, "ConfigRendered")
			Expect(configRendered).NotTo(BeNil())
			Expect(configRendered.Status).To(Equal(metav1.ConditionTrue))

			By("checking Degraded is absent on the success path")
			degraded := apimeta.FindStatusCondition(spinstance.Status.Conditions, "Degraded")
			Expect(degraded).To(BeNil())

			By("checking Ready is not True before the Deployment reports Available")
			ready := apimeta.FindStatusCondition(spinstance.Status.Conditions, "Ready")
			if ready != nil {
				Expect(ready.Status).NotTo(Equal(metav1.ConditionTrue))
			}

			By("patching the Deployment's status to report Available=True")
			dep := &appsv1.Deployment{}
			depName := types.NamespacedName{Name: resourceName + "-sp", Namespace: resourceNamespace}
			Expect(k8sClient.Get(ctx, depName, dep)).To(Succeed())
			dep.Status.Conditions = []appsv1.DeploymentCondition{
				{Type: appsv1.DeploymentAvailable, Status: corev1.ConditionTrue},
			}
			Expect(k8sClient.Status().Update(ctx, dep)).To(Succeed())

			By("reconciling again so the Ready condition picks up the Deployment's availability")
			_, err = controllerReconciler.Reconcile(ctx, reconcile.Request{
				NamespacedName: typeNamespacedName,
			})
			Expect(err).NotTo(HaveOccurred())

			Expect(k8sClient.Get(ctx, typeNamespacedName, spinstance)).To(Succeed())
			ready = apimeta.FindStatusCondition(spinstance.Status.Conditions, "Ready")
			Expect(ready).NotTo(BeNil())
			Expect(ready.Status).To(Equal(metav1.ConditionTrue))
		})

		It("should set Degraded and create no objects when the credentials Secret is missing", func() {
			// This test uses its own SPInstance name and references a Secret
			// that is never created, rather than reusing the shared
			// "test-resource" fixture: envtest runs with no garbage-collector
			// controller, so ConfigMap/Deployment/Service objects created by
			// other specs against "test-resource-sp" persist across specs, and
			// the no-objects-created assertions below need a name that no
			// other spec has ever reconciled successfully.
			const missingSecretResourceName = "test-resource-missing-secret"
			missingSecretNamespacedName := types.NamespacedName{
				Name:      missingSecretResourceName,
				Namespace: resourceNamespace,
			}

			By("creating an SPInstance referencing a credentials Secret that does not exist")
			resource := &samlv1alpha1.SPInstance{
				ObjectMeta: metav1.ObjectMeta{
					Name:      missingSecretResourceName,
					Namespace: resourceNamespace,
				},
				Spec: samlv1alpha1.SPInstanceSpec{
					EntityID:    "https://sp.example.com/shibboleth-missing-secret",
					Credentials: samlv1alpha1.SecretReference{Name: "sp-keypair-does-not-exist"},
					IdP: samlv1alpha1.IdPConfig{
						MetadataURL: "https://mocksaml.com/api/saml/metadata",
					},
				},
			}
			Expect(k8sClient.Create(ctx, resource)).To(Succeed())
			DeferCleanup(func() {
				Expect(k8sClient.Delete(ctx, resource)).To(Succeed())
			})

			By("Reconciling the created resource")
			controllerReconciler := &SPInstanceReconciler{
				Client:  k8sClient,
				Scheme:  k8sClient.Scheme(),
				SPImage: spImageTest,
			}

			result, err := controllerReconciler.Reconcile(ctx, reconcile.Request{
				NamespacedName: missingSecretNamespacedName,
			})
			Expect(err).NotTo(HaveOccurred())

			By("checking the reconcile Result requeues instead of waiting on a Secret watch")
			Expect(result.RequeueAfter).To(BeNumerically(">", 0))

			By("checking Degraded=True with a reason naming the missing Secret")
			Expect(k8sClient.Get(ctx, missingSecretNamespacedName, resource)).To(Succeed())
			degraded := apimeta.FindStatusCondition(resource.Status.Conditions, "Degraded")
			Expect(degraded).NotTo(BeNil())
			Expect(degraded.Status).To(Equal(metav1.ConditionTrue))
			Expect(degraded.Reason).To(Equal("CredentialsSecretMissing"))
			Expect(degraded.Message).To(ContainSubstring("sp-keypair-does-not-exist"))

			By("checking ObservedGeneration advanced on the Degraded path")
			Expect(resource.Status.ObservedGeneration).To(Equal(resource.Generation))

			By("checking no ConfigMap, Deployment, or Services were created")
			cm := &corev1.ConfigMap{}
			cmName := types.NamespacedName{Name: missingSecretResourceName + "-sp", Namespace: resourceNamespace}
			Expect(k8sClient.Get(ctx, cmName, cm)).To(HaveOccurred())

			dep := &appsv1.Deployment{}
			depName := types.NamespacedName{Name: missingSecretResourceName + "-sp", Namespace: resourceNamespace}
			Expect(k8sClient.Get(ctx, depName, dep)).To(HaveOccurred())

			svc := &corev1.Service{}
			svcName := types.NamespacedName{Name: missingSecretResourceName + "-sp", Namespace: resourceNamespace}
			Expect(k8sClient.Get(ctx, svcName, svc)).To(HaveOccurred())

			headlessSvc := &corev1.Service{}
			headlessSvcName := types.NamespacedName{Name: missingSecretResourceName + "-sp-headless", Namespace: resourceNamespace}
			Expect(k8sClient.Get(ctx, headlessSvcName, headlessSvc)).To(HaveOccurred())
		})
	})
})

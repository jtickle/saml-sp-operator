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
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	samlv1alpha1 "github.com/jtickle/saml-sp-operator/api/v1alpha1"
)

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
		})
		It("should successfully reconcile the resource", func() {
			By("Reconciling the created resource")
			controllerReconciler := &SPInstanceReconciler{
				Client: k8sClient,
				Scheme: k8sClient.Scheme(),
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
				Client: k8sClient,
				Scheme: k8sClient.Scheme(),
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
	})
})

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

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"

	samlv1alpha1 "github.com/jtickle/saml-sp-operator/api/v1alpha1"
)

// reconcileConfigMap creates or updates the ConfigMap named "<sp.Name>-sp"
// holding the rendered Shibboleth SP config files as data, owned by sp so it
// is garbage-collected when the SPInstance is deleted.
func (r *SPInstanceReconciler) reconcileConfigMap(ctx context.Context, sp *samlv1alpha1.SPInstance, files map[string]string) (*corev1.ConfigMap, error) {
	cm := &corev1.ConfigMap{
		ObjectMeta: metav1.ObjectMeta{
			Name:      sp.Name + "-sp",
			Namespace: sp.Namespace,
		},
	}

	_, err := controllerutil.CreateOrUpdate(ctx, r.Client, cm, func() error {
		cm.Data = files
		return ctrl.SetControllerReference(sp, cm, r.Scheme)
	})
	if err != nil {
		return nil, err
	}

	return cm, nil
}

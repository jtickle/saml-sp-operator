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
	"fmt"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	apimeta "k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	logf "sigs.k8s.io/controller-runtime/pkg/log"

	samlv1alpha1 "github.com/jtickle/saml-sp-operator/api/v1alpha1"
)

// SPInstanceReconciler reconciles a SPInstance object
type SPInstanceReconciler struct {
	client.Client
	Scheme *runtime.Scheme

	// SPImage is the container image reference for the shib-authenticator
	// image run in the rendered Deployment's "sp" container. Populated by
	// the CLI flag wired in a later task; envtest and other callers set it
	// directly.
	SPImage string
}

// +kubebuilder:rbac:groups=saml.tickletechnologies.com,resources=spinstances,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=saml.tickletechnologies.com,resources=spinstances/status,verbs=get;update;patch
// +kubebuilder:rbac:groups=saml.tickletechnologies.com,resources=spinstances/finalizers,verbs=update

// Condition types set on SPInstanceStatus.Conditions (DESIGN §7).
const (
	conditionConfigRendered = "ConfigRendered"
	conditionReady          = "Ready"
	conditionDegraded       = "Degraded"
)

// Reconcile is part of the main kubernetes reconciliation loop which aims to
// move the current state of the cluster closer to the desired state.
//
// Reconcile is fail-closed on the credentials Secret: it checks the Secret
// named by sp.Spec.Credentials exists in the SPInstance's namespace BEFORE
// rendering or creating anything. A missing Secret sets Degraded=True and
// returns without touching the ConfigMap, Deployment, or Services — a
// typo'd or not-yet-created Secret degrades visibly instead of producing a
// Deployment that mounts a Secret that isn't there.
//
// On the success path it renders the SP config, reconciles the ConfigMap
// holding it, the Deployment that runs it, and the ClusterIP and headless
// Services that front it, then records ConfigHash, ObservedGeneration, and
// the ConfigRendered/Ready conditions. Degraded is cleared on this path.
//
// For more details, check Reconcile and its Result here:
// - https://pkg.go.dev/sigs.k8s.io/controller-runtime@v0.24.1/pkg/reconcile
func (r *SPInstanceReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	log := logf.FromContext(ctx)

	sp := &samlv1alpha1.SPInstance{}
	if err := r.Get(ctx, req.NamespacedName, sp); err != nil {
		if apierrors.IsNotFound(err) {
			return ctrl.Result{}, nil
		}
		log.Error(err, "unable to fetch SPInstance")
		return ctrl.Result{}, err
	}

	credSecret := &corev1.Secret{}
	secretName := types.NamespacedName{Name: sp.Spec.Credentials.Name, Namespace: sp.Namespace}
	if err := r.Get(ctx, secretName, credSecret); err != nil {
		if !apierrors.IsNotFound(err) {
			log.Error(err, "unable to fetch credentials Secret")
			return ctrl.Result{}, err
		}

		apimeta.SetStatusCondition(&sp.Status.Conditions, metav1.Condition{
			Type:               conditionDegraded,
			Status:             metav1.ConditionTrue,
			Reason:             "CredentialsSecretMissing",
			Message:            fmt.Sprintf("credentials Secret %q not found in namespace %q", sp.Spec.Credentials.Name, sp.Namespace),
			ObservedGeneration: sp.Generation,
		})
		if err := r.Status().Update(ctx, sp); err != nil {
			log.Error(err, "unable to update SPInstance status")
			return ctrl.Result{}, err
		}
		return ctrl.Result{}, nil
	}

	files, hash, err := renderConfig(sp)
	if err != nil {
		log.Error(err, "unable to render SP config")
		return ctrl.Result{}, err
	}

	if _, err := r.reconcileConfigMap(ctx, sp, files); err != nil {
		log.Error(err, "unable to reconcile ConfigMap")
		return ctrl.Result{}, err
	}

	dep, err := r.reconcileDeployment(ctx, sp, hash)
	if err != nil {
		log.Error(err, "unable to reconcile Deployment")
		return ctrl.Result{}, err
	}

	if _, _, err := r.reconcileServices(ctx, sp); err != nil {
		log.Error(err, "unable to reconcile Services")
		return ctrl.Result{}, err
	}

	sp.Status.ConfigHash = hash
	sp.Status.ObservedGeneration = sp.Generation

	apimeta.SetStatusCondition(&sp.Status.Conditions, metav1.Condition{
		Type:               conditionConfigRendered,
		Status:             metav1.ConditionTrue,
		Reason:             conditionConfigRendered,
		Message:            "SP config rendered and reconciled to the ConfigMap, Deployment, and Services",
		ObservedGeneration: sp.Generation,
	})

	apimeta.RemoveStatusCondition(&sp.Status.Conditions, conditionDegraded)

	readyCondition := metav1.Condition{
		Type:               conditionReady,
		Status:             metav1.ConditionFalse,
		Reason:             "DeploymentProgressing",
		Message:            "Deployment has not yet reported Available",
		ObservedGeneration: sp.Generation,
	}
	if avail := deploymentAvailableCondition(dep); avail != nil && avail.Status == corev1.ConditionTrue {
		readyCondition.Status = metav1.ConditionTrue
		readyCondition.Reason = "DeploymentAvailable"
		readyCondition.Message = "Deployment reports Available"
	}
	apimeta.SetStatusCondition(&sp.Status.Conditions, readyCondition)

	if err := r.Status().Update(ctx, sp); err != nil {
		log.Error(err, "unable to update SPInstance status")
		return ctrl.Result{}, err
	}

	return ctrl.Result{}, nil
}

// deploymentAvailableCondition returns dep's "Available" DeploymentCondition,
// or nil if the Deployment controller hasn't reported one yet.
func deploymentAvailableCondition(dep *appsv1.Deployment) *appsv1.DeploymentCondition {
	for i := range dep.Status.Conditions {
		if dep.Status.Conditions[i].Type == appsv1.DeploymentAvailable {
			return &dep.Status.Conditions[i]
		}
	}
	return nil
}

// SetupWithManager sets up the controller with the Manager.
//
// The Owns watches are load-bearing for status: controller-runtime only
// re-triggers Reconcile for events on objects it watches. For(&SPInstance{})
// alone watches the SPInstance itself, so a change to an owned object that
// isn't also watched — e.g. the Deployment's own controller flipping
// Available — would never re-trigger Reconcile, and SPInstanceStatus.Ready
// would never update after rollout. Owns(&Deployment{}/&ConfigMap{}/
// &Service{}) closes that gap by watching owned-object events too and
// mapping them back to the owning SPInstance.
func (r *SPInstanceReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&samlv1alpha1.SPInstance{}).
		Owns(&appsv1.Deployment{}).
		Owns(&corev1.ConfigMap{}).
		Owns(&corev1.Service{}).
		Named("spinstance").
		Complete(r)
}

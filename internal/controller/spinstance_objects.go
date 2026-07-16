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

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/intstr"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"

	samlv1alpha1 "github.com/jtickle/saml-sp-operator/api/v1alpha1"
)

// spConfigHashAnnotation is the pod-template annotation that gates the
// Deployment rollout on the rendered SP config's content hash (SPI-02): a
// change to shibboleth2.xml, attribute-map.xml, or nginx.conf changes the
// hash, which changes this annotation, which changes the pod template and
// so triggers a rollout. An unchanged spec produces an unchanged hash and
// therefore no rollout on every reconcile.
const spConfigHashAnnotation = "saml.tickletechnologies.com/config-hash"

// volShibConfig, volSPCredentials, and volShibRun are the pod volume names
// shared between the Deployment's VolumeMounts and its Volumes — a mount and
// its volume must agree on the name, so each is referenced in both places.
const (
	volShibConfig    = "shib-config"
	volSPCredentials = "sp-credentials"
	volShibRun       = "shib-run"
)

// spPodLabels returns a fresh label map identifying the SP pods for sp. The
// Deployment's pod template and both Services' selectors all call this
// instead of sharing one map by reference, so none of them can mutate a
// map another object depends on.
func spPodLabels(sp *samlv1alpha1.SPInstance) map[string]string {
	return map[string]string{"app.kubernetes.io/name": "sp", "app.kubernetes.io/instance": sp.Name}
}

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

// reconcileDeployment creates or updates the Deployment named "<sp.Name>-sp"
// running the SP container, owned by sp so it is garbage-collected when the
// SPInstance is deleted.
//
// Three properties are load-bearing here:
//
//   - SPI-07 (fail-safe rollout): MaxUnavailable is pinned to 0 so a rolling
//     update never removes a working replica before its replacement passes
//     readiness — a bad SP config degrades capacity, never drops it to zero.
//   - SPI-02 (config-hash rollout gate): the pod template carries the
//     rendered config's content hash as an annotation. Kubernetes only
//     rolls a Deployment when its pod template changes, so this is what
//     turns a config edit into a rollout — and what keeps an unrelated
//     reconcile (same spec, same hash) from triggering one.
//   - SPI-03 (real readiness): the readiness probe execs curl against
//     shibd's own /Shibboleth.sso/Status endpoint inside the pod, so
//     "ready" means the SP process itself is answering, not that the
//     container merely started.
func (r *SPInstanceReconciler) reconcileDeployment(ctx context.Context, sp *samlv1alpha1.SPInstance, configHash string) (*appsv1.Deployment, error) {
	dep := &appsv1.Deployment{
		ObjectMeta: metav1.ObjectMeta{
			Name:      sp.Name + "-sp",
			Namespace: sp.Namespace,
		},
	}

	_, err := controllerutil.CreateOrUpdate(ctx, r.Client, dep, func() error {
		maxUnavailable := intstr.FromInt(0)

		dep.Spec.Selector = &metav1.LabelSelector{MatchLabels: spPodLabels(sp)}
		dep.Spec.Strategy = appsv1.DeploymentStrategy{
			Type: appsv1.RollingUpdateDeploymentStrategyType,
			RollingUpdate: &appsv1.RollingUpdateDeployment{
				MaxUnavailable: &maxUnavailable,
			},
		}

		dep.Spec.Template.ObjectMeta = metav1.ObjectMeta{
			Labels: spPodLabels(sp),
			Annotations: map[string]string{
				spConfigHashAnnotation: configHash,
			},
		}

		dep.Spec.Template.Spec.Containers = []corev1.Container{
			{
				Name:  "sp",
				Image: r.SPImage,
				Env: []corev1.EnvVar{
					{Name: "SHIBSP_SERVER_SCHEME", Value: "https"},
				},
				Ports: []corev1.ContainerPort{
					{ContainerPort: 8080},
				},
				ReadinessProbe: &corev1.Probe{
					ProbeHandler: corev1.ProbeHandler{
						Exec: &corev1.ExecAction{
							Command: []string{"curl", "-fsS", "http://localhost:8080/Shibboleth.sso/Status"},
						},
					},
					InitialDelaySeconds: 5,
					PeriodSeconds:       10,
					FailureThreshold:    3,
				},
				VolumeMounts: []corev1.VolumeMount{
					{Name: volShibConfig, MountPath: "/etc/shibboleth/shibboleth2.xml", SubPath: fileShibboleth2},
					{Name: volShibConfig, MountPath: "/etc/shibboleth/attribute-map.xml", SubPath: fileAttributeMap},
					{Name: volShibConfig, MountPath: "/etc/nginx/nginx.conf", SubPath: fileNginxConf},
					{Name: volSPCredentials, MountPath: "/run/shibboleth/sp-credentials", ReadOnly: true},
					{Name: volShibRun, MountPath: "/run/shibboleth"},
				},
			},
		}

		dep.Spec.Template.Spec.Volumes = []corev1.Volume{
			{
				Name: volShibConfig,
				VolumeSource: corev1.VolumeSource{
					ConfigMap: &corev1.ConfigMapVolumeSource{
						LocalObjectReference: corev1.LocalObjectReference{Name: sp.Name + "-sp"},
					},
				},
			},
			{
				Name: volSPCredentials,
				VolumeSource: corev1.VolumeSource{
					Secret: &corev1.SecretVolumeSource{
						SecretName: sp.Spec.Credentials.Name,
					},
				},
			},
			{
				Name: volShibRun,
				VolumeSource: corev1.VolumeSource{
					EmptyDir: &corev1.EmptyDirVolumeSource{},
				},
			},
		}

		return ctrl.SetControllerReference(sp, dep, r.Scheme)
	})
	if err != nil {
		return nil, err
	}

	return dep, nil
}

// reconcileServices creates or updates the ClusterIP Service named
// "<sp.Name>-sp" and the headless Service named "<sp.Name>-sp-headless",
// both selecting the SP pods and exposing port 8080, owned by sp so they
// are garbage-collected when the SPInstance is deleted.
//
// The ClusterIP Service is the stable address other workloads use to reach
// the SP. The headless Service (ClusterIP: None) resolves to pod endpoint
// IPs directly rather than a kube-proxy VIP — later slices' forward-auth
// dataplanes (Traefik headless target, Gateway API backendRef) target
// endpoints instead of the ClusterIP, per DESIGN.md's "target endpoints,
// not the ClusterIP" decision (spike fix O).
func (r *SPInstanceReconciler) reconcileServices(ctx context.Context, sp *samlv1alpha1.SPInstance) (*corev1.Service, *corev1.Service, error) {
	svc := &corev1.Service{
		ObjectMeta: metav1.ObjectMeta{
			Name:      sp.Name + "-sp",
			Namespace: sp.Namespace,
		},
	}

	_, err := controllerutil.CreateOrUpdate(ctx, r.Client, svc, func() error {
		svc.Spec.Selector = spPodLabels(sp)
		svc.Spec.Ports = []corev1.ServicePort{
			{Name: "http", Port: 8080, TargetPort: intstr.FromInt(8080)},
		}
		return ctrl.SetControllerReference(sp, svc, r.Scheme)
	})
	if err != nil {
		return nil, nil, err
	}

	headlessSvc := &corev1.Service{
		ObjectMeta: metav1.ObjectMeta{
			Name:      sp.Name + "-sp-headless",
			Namespace: sp.Namespace,
		},
	}

	_, err = controllerutil.CreateOrUpdate(ctx, r.Client, headlessSvc, func() error {
		headlessSvc.Spec.ClusterIP = corev1.ClusterIPNone
		headlessSvc.Spec.Selector = spPodLabels(sp)
		headlessSvc.Spec.Ports = []corev1.ServicePort{
			{Name: "http", Port: 8080, TargetPort: intstr.FromInt(8080)},
		}
		return ctrl.SetControllerReference(sp, headlessSvc, r.Scheme)
	})
	if err != nil {
		return nil, nil, err
	}

	return svc, headlessSvc, nil
}

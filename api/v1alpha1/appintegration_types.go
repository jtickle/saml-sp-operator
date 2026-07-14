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

package v1alpha1

import (
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
)

// EDIT THIS FILE!  THIS IS SCAFFOLDING FOR YOU TO OWN!
// NOTE: json tags are required.  Any new fields you add must have json tags for the fields to be serialized.

// AppIntegrationSpec defines the desired state of an AppIntegration. It lives in
// the app namespace beside the HTTPRoute it protects (Gateway API policy
// attachment is namespace-local by design — a targetRef has no namespace field).
// It binds that route to a central SPInstance, subject to the SPInstance's
// allowedNamespaces consent (DESIGN §5).
type AppIntegrationSpec struct {
	// spInstanceRef selects the SPInstance to bind to. It is cross-namespace
	// (the SPInstance lives in the auth namespace), so the namespace is explicit.
	SPInstanceRef SPInstanceReference `json:"spInstanceRef"`

	// targetRef references the app's HTTPRoute in THIS namespace. There is no
	// namespace field: attachment is namespace-local by Gateway API design.
	TargetRef TargetReference `json:"targetRef"`

	// attributes maps decoded SAML attribute ids to the request headers exported
	// to the app. Under Traefik ForwardAuth the exported header is engine-named
	// (Variable-<id>); the operator controls the <id> suffix and which attributes
	// flow, not the prefix (DESIGN §2, §9 addendum). Omit to export none.
	// +optional
	// +listType=map
	// +listMapKey=name
	Attributes []AttributeMapping `json:"attributes,omitempty"`

	// requireSession gates the app behind an authenticated session. When true
	// (the default when omitted), an unauthenticated request is redirected to the
	// IdP; when false the app is accessible but attributes flow when present.
	// +optional
	RequireSession *bool `json:"requireSession,omitempty"`

	// sessionPolicy overrides the SP-default session lifetime/timeout for this app.
	// +optional
	SessionPolicy *SessionPolicy `json:"sessionPolicy,omitempty"`

	// disableSingleLogout opts this app out of SP-initiated Single Logout
	// propagation (the SLO opt-out, DESIGN §5).
	// +optional
	DisableSingleLogout bool `json:"disableSingleLogout,omitempty"`
}

// SPInstanceReference names an SPInstance in another (the auth) namespace.
type SPInstanceReference struct {
	// name of the SPInstance.
	// +kubebuilder:validation:MinLength=1
	Name string `json:"name"`

	// namespace of the SPInstance (the auth namespace).
	// +kubebuilder:validation:MinLength=1
	Namespace string `json:"namespace"`
}

// TargetReference points at the HTTPRoute this AppIntegration protects, in the
// same namespace. Defaults describe a Gateway API HTTPRoute.
type TargetReference struct {
	// group of the target. Defaults to the Gateway API group.
	// +kubebuilder:default=gateway.networking.k8s.io
	// +optional
	Group string `json:"group,omitempty"`

	// kind of the target. Defaults to HTTPRoute.
	// +kubebuilder:default=HTTPRoute
	// +optional
	Kind string `json:"kind,omitempty"`

	// name of the target HTTPRoute.
	// +kubebuilder:validation:MinLength=1
	Name string `json:"name"`
}

// AttributeMapping maps one SAML attribute id to an exported header name.
type AttributeMapping struct {
	// name is the SAML attribute id (as decoded by attribute-map.xml), e.g. "email".
	// +kubebuilder:validation:MinLength=1
	Name string `json:"name"`

	// header is the request header exported to the app for this attribute.
	// +kubebuilder:validation:MinLength=1
	Header string `json:"header"`
}

// SessionPolicy tunes session duration.
type SessionPolicy struct {
	// lifetime is the absolute maximum session duration.
	// +optional
	Lifetime *metav1.Duration `json:"lifetime,omitempty"`

	// timeout is the idle (inactivity) timeout.
	// +optional
	Timeout *metav1.Duration `json:"timeout,omitempty"`
}

// AppIntegrationStatus defines the observed state of AppIntegration.
type AppIntegrationStatus struct {
	// INSERT ADDITIONAL STATUS FIELD - define observed state of cluster
	// Important: Run "make" to regenerate code after modifying this file

	// For Kubernetes API conventions, see:
	// https://github.com/kubernetes/community/blob/master/contributors/devel/sig-architecture/api-conventions.md#typical-status-properties

	// conditions represent the current state of the AppIntegration resource.
	// Each condition has a unique type and reflects the status of a specific aspect of the resource.
	//
	// Standard condition types include:
	// - "Available": the resource is fully functional
	// - "Progressing": the resource is being created or updated
	// - "Degraded": the resource failed to reach or maintain its desired state
	//
	// The status of each condition is one of True, False, or Unknown.
	// Condition types: SPInstanceResolved, RouteResolved, Conflict, Degraded,
	// Ready (DESIGN §7).
	// +listType=map
	// +listMapKey=type
	// +optional
	Conditions []metav1.Condition `json:"conditions,omitempty"`

	// observedGeneration is the .metadata.generation last reconciled.
	// +optional
	ObservedGeneration int64 `json:"observedGeneration,omitempty"`

	// resolvedHostnames lists the hostnames from the target HTTPRoute that this
	// integration contributes to the SP RequestMap.
	// +optional
	// +listType=set
	ResolvedHostnames []string `json:"resolvedHostnames,omitempty"`
}

// +kubebuilder:object:root=true
// +kubebuilder:subresource:status
// +kubebuilder:resource:shortName=appint
// +kubebuilder:printcolumn:name="SPInstance",type=string,JSONPath=`.spec.spInstanceRef.name`
// +kubebuilder:printcolumn:name="Route",type=string,JSONPath=`.spec.targetRef.name`
// +kubebuilder:printcolumn:name="Ready",type=string,JSONPath=`.status.conditions[?(@.type=="Ready")].status`
// +kubebuilder:printcolumn:name="Age",type=date,JSONPath=`.metadata.creationTimestamp`

// AppIntegration is the Schema for the appintegrations API
type AppIntegration struct {
	metav1.TypeMeta `json:",inline"`

	// metadata is a standard object metadata
	// +optional
	metav1.ObjectMeta `json:"metadata,omitzero"`

	// spec defines the desired state of AppIntegration
	// +required
	Spec AppIntegrationSpec `json:"spec"`

	// status defines the observed state of AppIntegration
	// +optional
	Status AppIntegrationStatus `json:"status,omitzero"`
}

// +kubebuilder:object:root=true

// AppIntegrationList contains a list of AppIntegration
type AppIntegrationList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitzero"`
	Items           []AppIntegration `json:"items"`
}

func init() {
	SchemeBuilder.Register(func(s *runtime.Scheme) error {
		s.AddKnownTypes(SchemeGroupVersion, &AppIntegration{}, &AppIntegrationList{})
		return nil
	})
}

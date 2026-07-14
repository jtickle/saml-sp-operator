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

// SPInstanceSpec defines the desired state of an SPInstance — one SAML Service
// Provider deployment. It lives in the central auth namespace so the SP private
// key is not readable from tenant namespaces (DESIGN §5). Per Shibboleth
// guidance the deployment advertises ONE entityID; apps are differentiated by
// per-path content settings, not separate SAML entities.
type SPInstanceSpec struct {
	// entityID is the SAML entityID advertised by the whole SP deployment.
	// One entityID per deployment — do NOT mint one per app (federation
	// anti-pattern, DESIGN §5).
	// +kubebuilder:validation:MinLength=1
	EntityID string `json:"entityID"`

	// credentials references the Secret in this namespace holding the SP signing/
	// encryption keypair. Conventional keys: tls.crt and tls.key.
	Credentials SecretReference `json:"credentials"`

	// idp configures the trusted identity provider(s): a single IdP by metadata,
	// or a signed federation metadata feed with a verification certificate.
	IdP IdPConfig `json:"idp"`

	// allowedNamespaces is the consent selector deciding which app namespaces may
	// bind an AppIntegration to this SPInstance. A nil selector denies all
	// cross-namespace binds; an empty selector ({}) allows all. This prevents an
	// app team from binding another tenant's federation trust by name (DESIGN §5).
	// +optional
	AllowedNamespaces *metav1.LabelSelector `json:"allowedNamespaces,omitempty"`

	// sessionStore references the shared session store (memcached) that lets
	// sessions and Single Logout work across SP replicas. Omit for a single-
	// replica in-memory store (no HA, no cross-replica SLO).
	// +optional
	SessionStore *SessionStoreConfig `json:"sessionStore,omitempty"`
}

// SecretReference names a Secret in the same namespace as the referring object.
type SecretReference struct {
	// name of the Secret.
	// +kubebuilder:validation:MinLength=1
	Name string `json:"name"`
}

// IdPConfig describes the identity-provider trust. A single metadataURL covers
// both the single-IdP and federation-feed cases; a signed federation feed
// additionally sets signingCert, and entityID pins one IdP within the feed.
type IdPConfig struct {
	// metadataURL is the URL of the IdP metadata or the federation metadata feed.
	// Egress-restricted clusters need an egress allowance or an in-cluster mirror
	// to reach this (DESIGN §11).
	// +kubebuilder:validation:MinLength=1
	MetadataURL string `json:"metadataURL"`

	// signingCert references a Secret holding the certificate used to verify a
	// signed federation metadata feed. Required for federation feeds; omit for a
	// single unsigned IdP metadata document.
	// +optional
	SigningCert *SecretReference `json:"signingCert,omitempty"`

	// entityID pins a single IdP entityID to trust within a multi-entity
	// federation feed. Omit to trust the whole feed.
	// +optional
	EntityID string `json:"entityID,omitempty"`
}

// SessionStoreConfig points at the shared memcached used for the session cache
// and replay cache.
type SessionStoreConfig struct {
	// memcachedServers is the list of host:port memcached endpoints.
	// +kubebuilder:validation:MinItems=1
	MemcachedServers []string `json:"memcachedServers"`
}

// SPInstanceStatus defines the observed state of SPInstance.
type SPInstanceStatus struct {
	// INSERT ADDITIONAL STATUS FIELD - define observed state of cluster
	// Important: Run "make" to regenerate code after modifying this file

	// For Kubernetes API conventions, see:
	// https://github.com/kubernetes/community/blob/master/contributors/devel/sig-architecture/api-conventions.md#typical-status-properties

	// conditions represent the current state of the SPInstance resource.
	// Each condition has a unique type and reflects the status of a specific aspect of the resource.
	//
	// Standard condition types include:
	// - "Available": the resource is fully functional
	// - "Progressing": the resource is being created or updated
	// - "Degraded": the resource failed to reach or maintain its desired state
	//
	// The status of each condition is one of True, False, or Unknown.
	// Condition types: ConfigRendered, RolloutReady, Ready, Degraded (DESIGN §7).
	// +listType=map
	// +listMapKey=type
	// +optional
	Conditions []metav1.Condition `json:"conditions,omitempty"`

	// observedGeneration is the .metadata.generation last reconciled.
	// +optional
	ObservedGeneration int64 `json:"observedGeneration,omitempty"`

	// boundCount is the number of AppIntegrations currently aggregated into this
	// SP's rendered config.
	// +optional
	BoundCount int32 `json:"boundCount,omitempty"`

	// metadataURL is the SP metadata URL to hand to IdP/federation admins for
	// registration — surfaced in status so it can be read straight from the
	// object (DESIGN §7).
	// +optional
	MetadataURL string `json:"metadataURL,omitempty"`

	// configHash is the hash of the rendered shibboleth2.xml + nginx.conf. The
	// Deployment rolls only when this changes, so unrelated AppIntegration
	// reconciles don't churn the fleet (DESIGN §7).
	// +optional
	ConfigHash string `json:"configHash,omitempty"`
}

// +kubebuilder:object:root=true
// +kubebuilder:subresource:status
// +kubebuilder:resource:shortName=spi
// +kubebuilder:printcolumn:name="EntityID",type=string,JSONPath=`.spec.entityID`
// +kubebuilder:printcolumn:name="Bound",type=integer,JSONPath=`.status.boundCount`
// +kubebuilder:printcolumn:name="Ready",type=string,JSONPath=`.status.conditions[?(@.type=="Ready")].status`
// +kubebuilder:printcolumn:name="Age",type=date,JSONPath=`.metadata.creationTimestamp`

// SPInstance is the Schema for the spinstances API
type SPInstance struct {
	metav1.TypeMeta `json:",inline"`

	// metadata is a standard object metadata
	// +optional
	metav1.ObjectMeta `json:"metadata,omitzero"`

	// spec defines the desired state of SPInstance
	// +required
	Spec SPInstanceSpec `json:"spec"`

	// status defines the observed state of SPInstance
	// +optional
	Status SPInstanceStatus `json:"status,omitzero"`
}

// +kubebuilder:object:root=true

// SPInstanceList contains a list of SPInstance
type SPInstanceList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitzero"`
	Items           []SPInstance `json:"items"`
}

func init() {
	SchemeBuilder.Register(func(s *runtime.Scheme) error {
		s.AddKnownTypes(SchemeGroupVersion, &SPInstance{}, &SPInstanceList{})
		return nil
	})
}

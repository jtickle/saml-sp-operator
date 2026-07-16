// Package render turns SP identity and resolved app bindings into byte-correct,
// injection-safe Shibboleth SP v3 config output (shibboleth2.xml,
// attribute-map.xml, nginx.conf), plus deterministic RequestMap collision
// resolution.
//
// This package has ZERO Kubernetes runtime dependency (D-01/D-02): it never
// imports k8s.io/apimachinery, k8s.io/client-go, sigs.k8s.io/controller-runtime,
// or api/v1alpha1. Every input a controller would otherwise supply from a CRD
// spec or ObjectMeta is synthesized into a plain-Go struct by the caller before
// it crosses into this package (e.g. AppBinding.CreatedAtUnix is truncated from
// metav1.Time by the caller; AppBinding.UID is a bare string, not types.UID).
//
// This boundary is deliberate, not arbitrary purity: this package is the
// shared seam between the operator's controllers (which will import it from
// Phase 3 onward so they can never disagree about a RequestMap collision
// winner, see Resolve) and a planned standalone single-container deployment
// (nginx auth_request attachment) that will consume the same rendering core
// without a base-container merge. See DESIGN.md — the own-orchestration /
// borrow-SAML framing and the shared render seam — for the authoritative
// rationale behind this k8s-free boundary.
package render

// SPConfig is the plain-Go input describing one SP deployment's identity,
// credentials, and session behavior. It mirrors SPInstanceSpec
// (api/v1alpha1/spinstance_types.go) field-for-field without importing it.
type SPConfig struct {
	// EntityID is the SAML entityID advertised by the whole SP deployment.
	EntityID string

	// IdP configures the trusted identity provider.
	IdP IdPConfig

	// CredentialKeyPath and CredentialCertPath are caller-resolved filesystem
	// paths to the SP signing/decryption keypair. The caller reads the Secret
	// referenced by SPInstanceSpec.Credentials and resolves it to an in-pod
	// mount path; this package never performs Secret I/O.
	CredentialKeyPath  string
	CredentialCertPath string

	// RemoteUser is the ordered list of attribute ids tried, in order, for the
	// principal name (shibboleth2.xml ApplicationDefaults REMOTE_USER — the
	// renderer space-joins this slice at render time).
	RemoteUser []string

	// Sessions tunes session lifetime/timeout/behavior for shibboleth2.xml's
	// <Sessions> element.
	Sessions SessionDefaults
}

// IdPConfig describes the trusted identity provider. It mirrors
// SPInstanceSpec.IdP (api/v1alpha1/spinstance_types.go) without importing it.
// SigningCert is intentionally omitted here: verifying a signed federation
// metadata feed is a caller/controller concern (Secret resolution), not
// something this package fetches or validates.
type IdPConfig struct {
	// MetadataURL is the URL of the IdP metadata or federation metadata feed.
	MetadataURL string

	// EntityID pins a single IdP entityID to trust within a multi-entity
	// federation feed. Empty means trust the whole feed.
	EntityID string
}

// SessionDefaults tunes shibboleth2.xml's <Sessions> element. Lifetime and
// Timeout are whole seconds (not metav1.Duration) so this package stays
// k8s-free — the caller converts any *metav1.Duration input to int64 seconds
// before it crosses the boundary, the same convention AppBinding.CreatedAtUnix
// uses for metav1.Time (D-01).
type SessionDefaults struct {
	LifetimeSeconds int64
	TimeoutSeconds  int64

	// RelayState selects the SP's relay-state storage mechanism (e.g. "ss:mem").
	RelayState string

	// CheckAddress, when false, disables the SP's IP-address session-binding
	// check — required when the client IP seen by the SP is the gateway's, not
	// the browser's (spike finding: the operator always sits behind a gateway).
	CheckAddress bool

	// HandlerSSL and CookieProps tell the SP it is effectively on HTTPS even
	// when TLS is terminated in front of it by the gateway.
	HandlerSSL  bool
	CookieProps string
}

// AttributeMapping maps one incoming SAML attribute to the id shibd re-exports
// (the app receives it as the header "Variable-<ExportedID>"). It mirrors
// api/v1alpha1's AttributeMapping (used by both AppIntegrationSpec.Attributes
// and, as a slice, SPConfig-adjacent SP-wide attribute-map.xml rendering)
// without importing it.
type AttributeMapping struct {
	// Name is the SAML attribute id (as decoded by attribute-map.xml), e.g. "email".
	Name string

	// ExportedID is the attribute id shibd re-exports; the app receives it as
	// the header "Variable-<ExportedID>".
	ExportedID string
}

// AppBinding is the plain-Go synthesized input describing one app's
// contribution to the SP RequestMap. It carries fields that do not exist
// together on any single CRD object: (Hostname, Path) is resolved from the
// app's HTTPRoute by the controller (it does not exist on AppIntegrationSpec
// at all), while CreatedAtUnix and UID live on ObjectMeta, not spec. This
// package needs a synthesized input regardless of the k8s-dependency
// question (D-01).
type AppBinding struct {
	// Namespace and Name identify the source AppIntegration (for Conflict
	// reporting and log/status correlation) — never used as a Resolve sort key.
	Namespace string
	Name      string

	// UID is the source AppIntegration's ObjectMeta.UID, a bare string (not
	// types.UID) so this package avoids the apimachinery type import. It is
	// the final Resolve sort-key tiebreak (D-07).
	UID string

	// Hostname, Path, Scheme, and Port describe this binding's RequestMap
	// entry. Scheme and Port are mandatory on the rendered <Host> even at
	// default values (spike fix N — a bare <Host> auto-expands only to
	// standard ports and silently fails OPEN on a non-standard port).
	Hostname string
	Path     string
	Scheme   string
	Port     int

	// Priority is the self-asserted precedence used first in Resolve's sort
	// key (higher wins, default 0). It mirrors a future
	// AppIntegration.spec.priority int32 field (D-07). It cannot be used to
	// steal a hostname: two bindings can only collide on (Hostname, Path) if
	// both already hold a Gateway-API-attached HTTPRoute for that hostname,
	// so Priority only orders precedence among already-authorized routes
	// (see this plan's threat_model T-01-01).
	Priority int32

	// CreatedAtUnix is the source AppIntegration's ObjectMeta.CreationTimestamp,
	// pre-truncated by the caller to whole unix seconds (metav1.Time is
	// second-granular already, so this is a lossless truncation, not an
	// approximation). It is Resolve's second sort key, after Priority.
	CreatedAtUnix int64

	// RequireSession gates the app behind an authenticated session. The
	// caller resolves AppIntegrationSpec.RequireSession's *bool
	// omit-means-default-true convention before this boundary — this package
	// never encodes that convention itself.
	RequireSession bool

	// Attributes are the SAML attribute id -> header exports for this app.
	Attributes []AttributeMapping
}

// Resolution is Resolve's output: the deterministic RequestMap winners plus
// every collision loser, each carrying its winner for status/Conflict
// reporting (D-06).
type Resolution struct {
	Winners   []AppBinding
	Conflicts []Conflict
}

// Conflict records one (Hostname, Path) collision: the AppBinding that won
// (also present in Resolution.Winners) and the identity of the AppBinding
// that lost. Losers never appear in Resolution.Winners.
type Conflict struct {
	Winner AppBinding

	LoserNamespace string
	LoserName      string
	LoserUID       string

	Hostname string
	Path     string
}

// AttachmentModel identifies which gateway forward-auth mechanism is
// consuming this SP's rendered output. RENDER-08's clear-list computation
// branches on this value: Traefik ForwardAuth cannot wildcard-strip inbound
// headers (the clear-list must enumerate them), while nginx auth_request can
// use a glob.
type AttachmentModel string

const (
	// TraefikForwardAuth targets a headless Service behind Traefik's
	// ForwardAuth Middleware (this operator's v1 attachment).
	TraefikForwardAuth AttachmentModel = "traefik-forward-auth"

	// NginxAuthRequest targets nginx's auth_request directive (the planned
	// standalone single-container deployment's attachment, D-02).
	NginxAuthRequest AttachmentModel = "nginx-auth-request"
)

// ClearListSpec is RENDER-08's per-attachment-model edge header hygiene
// input: the set of client-injected identity headers that must never reach
// the app unclobbered by the SP's own exports.
type ClearListSpec struct {
	Model AttachmentModel

	// Headers is the explicit header list (required for TraefikForwardAuth,
	// which cannot wildcard-strip).
	Headers []string

	// Glob is the wildcard pattern (usable for NginxAuthRequest, which can
	// strip by glob).
	Glob string
}

// ConfigFile is one named rendered artifact's bytes — the shared shape
// consumed by both this package's renderers (RenderShibboleth2,
// RenderAttributeMap, RenderNginxConf) and Hash (D-08's config-hash input,
// RENDER-09).
type ConfigFile struct {
	Name  string
	Bytes []byte
}

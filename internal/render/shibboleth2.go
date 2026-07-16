package render

import (
	"encoding/xml"
	"slices"
	"strings"
)

// shibboleth2.go renders shibboleth2.xml (RENDER-01) — the full Shibboleth
// SP v3 config tree via encoding/xml struct marshaling — with the
// RequestMap aggregated and deterministically ordered from Resolve's
// Winners (RENDER-04), and every <Host> carrying an explicit scheme+port
// even on default ports (RENDER-05, spike fix N).
//
// Golden-fixture scope (RESEARCH.md Pitfall 5 / Open Question 1, locked by
// this plan): the byte-compare target is testdata/golden/shibboleth2.xml,
// the package's own comment-free, machine-formatted semantic-tree
// reproduction of the repo-root shibboleth2.xml — NOT the repo-root file's
// hand-authored prose comments or manual formatting. The repo-root file is
// the structural reference only. Do not attempt to regenerate its prose.
//
// Elements this file's tree carries that have no corresponding SPConfig
// field (Errors, MetadataProvider.backingFilePath/maxRefreshDelay,
// AttributeExtractor/Resolver/Filter, SecurityPolicyProvider,
// ProtocolProvider, clockSkew, Handler ACLs) are rendered as fixed
// structural constants below, matching the repo-root fixture's shape. This
// plan's files_modified is scoped to shibboleth2.go/_test.go/testdata only
// — it does not extend SPConfig with per-deployment knobs for values the
// spike fixture itself never varied.
//
// No element in this tree is rendered as an XML comment, so the D-05 "--"-
// in-comment guard (struct-tag `,comment` path; RESEARCH.md Pitfall 4) has
// no applicable call site here — every CRD-derived string in this tree
// flows through a normal attr/chardata struct-tag field, which
// encoding/xml auto-escapes by construction (T-03-02 mitigation). If a
// future revision adds an operator-generated comment, route it through a
// field tagged `xml:",comment"` (which rejects any "--"), never through a
// manual xml.EncodeToken(xml.Comment(...)) call.
const (
	shibAuthType = "shibboleth"

	shibClockSkew = "180"

	shibErrorsSupportContact = "root@localhost"
	shibErrorsHelpLocation   = "/about.html"
	shibErrorsStyleSheet     = "/shibboleth-sp/main.css"

	shibMetadataProviderBackingFilePath = "/run/shibboleth/idp-metadata.xml"
	shibMetadataProviderMaxRefreshDelay = "3600"

	shibAttributeMapPath      = "attribute-map.xml"
	shibAttributeFilterPath   = "attribute-policy.xml"
	shibSecurityPolicyPath    = "security-policy.xml"
	shibProtocolProviderPath  = "protocols.xml"
	shibAttributeResolverType = "Query"

	// relativeHandlerURL is RELATIVE so one SP spans multiple app hosts: the
	// SP reconstructs the per-request host itself (verified:
	// shibmultihost_test.go). SHIBSP_SERVER_SCHEME=https (pod env) forces the
	// scheme; no host is pinned.
	relativeHandlerURL = "/Shibboleth.sso"
)

// hostXML is the RequestMap <Host> element shape (RESEARCH.md Code
// Examples hostElement, RENDER-05): scheme and port are ALWAYS emitted,
// even at the scheme-default port — never omitted or left to bare-hostname
// auto-expansion (spike fix N, T-03-01).
//
// A single-binding, whole-host group (Path == "" or "/") renders its
// authType/requireSession directly on <Host>, matching the repo-root
// fixture's single-app shape. A host with more than one distinct Path (or
// a single non-root Path) renders nested <Path> children instead, ordered
// most-specific-path-first (RENDER-04); AuthType/RequireSession are then
// omitted from the parent <Host> since Path-level attributes are ambiguous
// per binding.
type hostXML struct {
	Name           string    `xml:"name,attr"`
	Scheme         string    `xml:"scheme,attr"`
	Port           int       `xml:"port,attr"`
	AuthType       string    `xml:"authType,attr,omitempty"`
	RequireSession string    `xml:"requireSession,attr,omitempty"`
	Paths          []pathXML `xml:"Path,omitempty"`
}

// pathXML is a nested RequestMap <Path> element, used when a Host has more
// than one distinct (Hostname, Path) winner (RENDER-04).
type pathXML struct {
	Name           string `xml:"name,attr"`
	AuthType       string `xml:"authType,attr,omitempty"`
	RequireSession string `xml:"requireSession,attr,omitempty"`
}

type requestMapXML struct {
	Hosts []hostXML `xml:"Host"`
}

type requestMapperXML struct {
	Type       string        `xml:"type,attr"`
	RequestMap requestMapXML `xml:"RequestMap"`
}

type ssoXML struct {
	EntityID string `xml:"entityID,attr"`
	Value    string `xml:",chardata"`
}

type logoutXML struct {
	Value string `xml:",chardata"`
}

type handlerXML struct {
	Type                string `xml:"type,attr"`
	Location            string `xml:"Location,attr"`
	Signing             string `xml:"signing,attr,omitempty"`
	ACL                 string `xml:"acl,attr,omitempty"`
	ShowAttributeValues string `xml:"showAttributeValues,attr,omitempty"`
}

type sessionsXML struct {
	Lifetime     int64        `xml:"lifetime,attr"`
	Timeout      int64        `xml:"timeout,attr"`
	RelayState   string       `xml:"relayState,attr"`
	CheckAddress string       `xml:"checkAddress,attr"`
	HandlerSSL   string       `xml:"handlerSSL,attr"`
	CookieProps  string       `xml:"cookieProps,attr"`
	HandlerURL   string       `xml:"handlerURL,attr"`
	SSO          ssoXML       `xml:"SSO"`
	Logout       logoutXML    `xml:"Logout"`
	Handlers     []handlerXML `xml:"Handler"`
}

type errorsXML struct {
	SupportContact string `xml:"supportContact,attr"`
	HelpLocation   string `xml:"helpLocation,attr"`
	StyleSheet     string `xml:"styleSheet,attr"`
}

type metadataProviderXML struct {
	Type            string `xml:"type,attr"`
	Validate        string `xml:"validate,attr"`
	URL             string `xml:"url,attr"`
	BackingFilePath string `xml:"backingFilePath,attr"`
	MaxRefreshDelay string `xml:"maxRefreshDelay,attr"`
}

type attributeExtractorXML struct {
	Type          string `xml:"type,attr"`
	Validate      string `xml:"validate,attr"`
	ReloadChanges string `xml:"reloadChanges,attr"`
	Path          string `xml:"path,attr"`
}

type attributeResolverXML struct {
	Type         string `xml:"type,attr"`
	SubjectMatch string `xml:"subjectMatch,attr"`
}

type attributeFilterXML struct {
	Type     string `xml:"type,attr"`
	Validate string `xml:"validate,attr"`
	Path     string `xml:"path,attr"`
}

type credentialResolverXML struct {
	Type        string `xml:"type,attr"`
	Key         string `xml:"key,attr"`
	Certificate string `xml:"certificate,attr"`
}

type applicationDefaultsXML struct {
	EntityID           string                `xml:"entityID,attr"`
	RemoteUser         string                `xml:"REMOTE_USER,attr"`
	Sessions           sessionsXML           `xml:"Sessions"`
	Errors             errorsXML             `xml:"Errors"`
	MetadataProvider   metadataProviderXML   `xml:"MetadataProvider"`
	AttributeExtractor attributeExtractorXML `xml:"AttributeExtractor"`
	AttributeResolver  attributeResolverXML  `xml:"AttributeResolver"`
	AttributeFilter    attributeFilterXML    `xml:"AttributeFilter"`
	CredentialResolver credentialResolverXML `xml:"CredentialResolver"`
}

type securityPolicyProviderXML struct {
	Type     string `xml:"type,attr"`
	Validate string `xml:"validate,attr"`
	Path     string `xml:"path,attr"`
}

type protocolProviderXML struct {
	Type          string `xml:"type,attr"`
	Validate      string `xml:"validate,attr"`
	ReloadChanges string `xml:"reloadChanges,attr"`
	Path          string `xml:"path,attr"`
}

// spConfigXML is the SPConfig root element. Per D-03, xmlns and xmlns:conf
// are declared as plain string attributes on this root struct ONLY — every
// descendant struct in this file carries a bare local-name tag (empty
// Space). Never use xml.Name{Space} here or on any child (Go issue #9519 —
// that approach re-declares xmlns on every child element).
type spConfigXML struct {
	XMLName                xml.Name                  `xml:"SPConfig"`
	Xmlns                  string                    `xml:"xmlns,attr"`
	XmlnsConf              string                    `xml:"xmlns:conf,attr"`
	ClockSkew              string                    `xml:"clockSkew,attr"`
	RequestMapper          requestMapperXML          `xml:"RequestMapper"`
	ApplicationDefaults    applicationDefaultsXML    `xml:"ApplicationDefaults"`
	SecurityPolicyProvider securityPolicyProviderXML `xml:"SecurityPolicyProvider"`
	ProtocolProvider       protocolProviderXML       `xml:"ProtocolProvider"`
}

// boolAttr renders a Go bool as the lowercase XML-attribute string shibd
// expects ("true"/"false"), never Go's %v formatting (which happens to
// match here, but this makes the contract explicit rather than incidental).
func boolAttr(b bool) string {
	if b {
		return "true"
	}
	return "false"
}

// pathDepth returns the number of non-empty "/"-delimited segments in p,
// used only to rank Path specificity (RENDER-04) — never for security
// decisions (that is shibd's own longest-match logic at load time).
func pathDepth(p string) int {
	segs := strings.Split(strings.Trim(p, "/"), "/")
	n := 0
	for _, s := range segs {
		if s != "" {
			n++
		}
	}
	return n
}

// buildRequestMapHosts groups winners by Hostname and returns the ordered
// []hostXML for the RequestMap (RENDER-04): exact <Host> before
// <HostRegex> — this package's AppBinding has no regex-host input, so
// every group renders as an exact <Host> — and, within a host,
// most-specific-path-first. Hosts themselves are ordered by name for
// deterministic output. This never ranges a Go map to build the emitted
// order (only for grouping keys before a final deterministic sort of the
// group's own hostnames).
func buildRequestMapHosts(winners []AppBinding) []hostXML {
	if len(winners) == 0 {
		return nil
	}

	ordered := slices.Clone(winners)
	slices.SortFunc(ordered, func(a, b AppBinding) int {
		if c := strings.Compare(a.Hostname, b.Hostname); c != 0 {
			return c
		}
		if c := pathDepth(b.Path) - pathDepth(a.Path); c != 0 {
			return c // desc: most-specific (deepest) path first
		}
		return strings.Compare(a.Path, b.Path)
	})

	var hosts []hostXML
	i := 0
	for i < len(ordered) {
		j := i
		for j < len(ordered) && ordered[j].Hostname == ordered[i].Hostname {
			j++
		}
		group := ordered[i:j]

		if len(group) == 1 && (group[0].Path == "" || group[0].Path == "/") {
			b := group[0]
			hosts = append(hosts, hostXML{
				Name:           b.Hostname,
				Scheme:         b.Scheme,
				Port:           b.Port,
				AuthType:       shibAuthType,
				RequireSession: boolAttr(b.RequireSession),
			})
			i = j
			continue
		}

		h := hostXML{
			Name:   group[0].Hostname,
			Scheme: group[0].Scheme,
			Port:   group[0].Port,
		}
		for _, b := range group {
			h.Paths = append(h.Paths, pathXML{
				Name:           strings.TrimPrefix(b.Path, "/"),
				AuthType:       shibAuthType,
				RequireSession: boolAttr(b.RequireSession),
			})
		}
		hosts = append(hosts, h)
		i = j
	}

	return hosts
}

// buildShibboleth2Tree assembles the full shibboleth2.xml struct tree from
// cfg and winners. handlerURL is the fixed relativeHandlerURL (RENDER-02):
// one SP now spans multiple app hosts, so no single external host/port can
// be baked into handlerURL or nginx.conf's responder block — the SP
// reconstructs the per-request host itself (verified:
// shibmultihost_test.go). cfg.EntityID is rendered verbatim: it is already
// the complete SAML entityID literal per SPConfig's field contract
// (types.go).
func buildShibboleth2Tree(cfg SPConfig, winners []AppBinding) spConfigXML {
	return spConfigXML{
		Xmlns:     "urn:mace:shibboleth:3.0:native:sp:config",
		XmlnsConf: "urn:mace:shibboleth:3.0:native:sp:config",
		ClockSkew: shibClockSkew,
		RequestMapper: requestMapperXML{
			Type: "Native",
			RequestMap: requestMapXML{
				Hosts: buildRequestMapHosts(winners),
			},
		},
		ApplicationDefaults: applicationDefaultsXML{
			EntityID:   cfg.EntityID,
			RemoteUser: strings.Join(cfg.RemoteUser, " "),
			Sessions: sessionsXML{
				Lifetime:     cfg.Sessions.LifetimeSeconds,
				Timeout:      cfg.Sessions.TimeoutSeconds,
				RelayState:   cfg.Sessions.RelayState,
				CheckAddress: boolAttr(cfg.Sessions.CheckAddress),
				HandlerSSL:   boolAttr(cfg.Sessions.HandlerSSL),
				CookieProps:  cfg.Sessions.CookieProps,
				HandlerURL:   relativeHandlerURL,
				SSO: ssoXML{
					EntityID: cfg.IdP.EntityID,
					Value:    "SAML2",
				},
				Logout: logoutXML{Value: "SAML2 Local"},
				Handlers: []handlerXML{
					{Type: "MetadataGenerator", Location: "/Metadata", Signing: "false"},
					{Type: "Status", Location: "/Status", ACL: "127.0.0.1 ::1"},
					{Type: "Session", Location: "/Session", ShowAttributeValues: "true"},
				},
			},
			Errors: errorsXML{
				SupportContact: shibErrorsSupportContact,
				HelpLocation:   shibErrorsHelpLocation,
				StyleSheet:     shibErrorsStyleSheet,
			},
			MetadataProvider: metadataProviderXML{
				Type:            "XML",
				Validate:        "true",
				URL:             cfg.IdP.MetadataURL,
				BackingFilePath: shibMetadataProviderBackingFilePath,
				MaxRefreshDelay: shibMetadataProviderMaxRefreshDelay,
			},
			AttributeExtractor: attributeExtractorXML{
				Type:          "XML",
				Validate:      "true",
				ReloadChanges: "false",
				Path:          shibAttributeMapPath,
			},
			AttributeResolver: attributeResolverXML{
				Type:         shibAttributeResolverType,
				SubjectMatch: "true",
			},
			AttributeFilter: attributeFilterXML{
				Type:     "XML",
				Validate: "true",
				Path:     shibAttributeFilterPath,
			},
			CredentialResolver: credentialResolverXML{
				Type:        "File",
				Key:         cfg.CredentialKeyPath,
				Certificate: cfg.CredentialCertPath,
			},
		},
		SecurityPolicyProvider: securityPolicyProviderXML{
			Type:     "XML",
			Validate: "true",
			Path:     shibSecurityPolicyPath,
		},
		ProtocolProvider: protocolProviderXML{
			Type:          "XML",
			Validate:      "true",
			ReloadChanges: "false",
			Path:          shibProtocolProviderPath,
		},
	}
}

// RenderShibboleth2 renders the full shibboleth2.xml document for cfg and
// its resolved RequestMap winners (RENDER-01/04/05). Output equals
// testdata/golden/shibboleth2.xml byte-for-byte for the sample inputs
// exercised by TestRenderShibboleth2.
func RenderShibboleth2(cfg SPConfig, winners []AppBinding) ([]byte, error) {
	tree := buildShibboleth2Tree(cfg, winners)

	body, err := xml.MarshalIndent(tree, "", "    ")
	if err != nil {
		return nil, err
	}

	body = collapseEmptyElements(body)
	return withXMLProlog(body), nil
}

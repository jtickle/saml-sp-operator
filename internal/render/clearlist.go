package render

import "fmt"

// clearlist.go computes RENDER-08's per-attachment-model edge
// header-hygiene value: the set of client-injected identity headers that
// must never reach the app unclobbered by the SP's own Variable-* exports.
// The FastCGI shibauthorizer exports each decoded attribute (and the
// principal) as an HTTP response header named "Variable-<id>"
// (attribute-map.xml, repo root — see that file's own explanatory comment,
// lines 17-24).
//
// This file is a PURE VALUE computation only (RESEARCH.md Pitfall 6): it
// does not render a clear-list directive into nginx.conf — the repo-root
// nginx.conf fixture has no more_clear_input_headers/clear-list directive
// at all, and nginxconf.go does not add one — and there is no golden-file
// compare against nginx.conf for ClearList's output. The
// TraefikForwardAuth-enumerate branch's Middleware consumer lands in
// Phase 5; the NginxAuthRequest-glob branch's consumer is the future
// standalone single-container tool (D-02).
//
// AttributeMapping.ExportedID is the attribute id shibd re-exports.
// attributemap.go renders it verbatim into attribute-map.xml's
// <Attribute id=.../> (attributemap.go: `ID: a.ExportedID`), and ClearList
// applies the "Variable-" prefix here over that SAME field — so the two
// renderers can never disagree about an attribute's exported header name.

// remoteUserHeader is the principal identity header the FastCGI authorizer
// always exports (shibboleth2.xml's ApplicationDefaults REMOTE_USER),
// present regardless of which attributes an app declares.
const remoteUserHeader = "Variable-REMOTE_USER"

// ClearList computes the per-attachment-model edge header-hygiene value for
// attrs (RENDER-08). TraefikForwardAuth cannot wildcard-strip inbound
// headers, so its ClearListSpec enumerates every exported header name
// explicitly, in attrs' input slice order (never a Go map range).
// NginxAuthRequest can strip by glob, so its ClearListSpec carries a single
// Variable-* wildcard instead. An unrecognized model returns a non-nil
// error rather than silently defaulting to either shape.
func ClearList(model AttachmentModel, attrs []AttributeMapping) (ClearListSpec, error) {
	switch model {
	case TraefikForwardAuth:
		headers := make([]string, 0, len(attrs)+1)
		headers = append(headers, remoteUserHeader)
		for _, a := range attrs {
			headers = append(headers, "Variable-"+a.ExportedID)
		}
		return ClearListSpec{Model: model, Headers: headers, Glob: ""}, nil

	case NginxAuthRequest:
		return ClearListSpec{Model: model, Headers: nil, Glob: "Variable-*"}, nil

	default:
		return ClearListSpec{}, fmt.Errorf("render: unrecognized AttachmentModel %q", model)
	}
}

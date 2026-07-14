package render

import (
	"encoding/xml"
	"regexp"
)

// emptyElementRE matches an element whose open tag (name + zero-or-more
// quoted attributes) is immediately followed by its own matching close tag
// with no content between — i.e. an element encoding/xml marshaled as
// "<Foo attrs...></Foo>" with genuinely no chardata or child elements.
// Anchored on a closing-tag-name backreference so chardata-bearing elements
// like "<SSO ...>SAML2</SSO>" never match (there's content between the tags).
var emptyElementRE = regexp.MustCompile(`(?s)<([A-Za-z][\w:.-]*)((?:\s+[\w:.-]+="[^"]*")*)\s*></([A-Za-z][\w:.-]*)>`)

// collapseEmptyElements rewrites every genuinely-empty element in b from the
// paired "<Foo attrs...></Foo>" form encoding/xml always produces into the
// self-closing "<Foo attrs.../>" form the target config fixtures use.
// encoding/xml's Marshal/MarshalIndent never emit the self-closing form
// themselves (RESEARCH.md Pattern 1 / Pitfall 1, verified against
// src/encoding/xml/marshal.go — there is no code path that ever writes
// "/>") so this post-process is required for byte-for-byte reproduction of
// the shibboleth2.xml / attribute-map.xml golden fixtures.
func collapseEmptyElements(b []byte) []byte {
	return emptyElementRE.ReplaceAllFunc(b, func(m []byte) []byte {
		sub := emptyElementRE.FindSubmatch(m)
		name, attrs, closeName := sub[1], sub[2], sub[3]
		if string(name) != string(closeName) {
			// Defensive: malformed match (shouldn't happen given the
			// regex's own backreference-free structure guards name/
			// closeName independently), leave untouched rather than
			// risk corrupting a well-formed document.
			return m
		}
		return []byte("<" + string(name) + string(attrs) + "/>")
	})
}

// withXMLProlog prepends the XML declaration to a marshaled document body.
// encoding/xml deliberately never emits "<?xml version=\"1.0\"
// encoding=\"UTF-8\"?>" itself (RESEARCH.md Pitfall 2) — xml.Header exists
// specifically so callers who want it can add it, and its value already
// includes the trailing newline matching the fixture format.
func withXMLProlog(body []byte) []byte {
	return append([]byte(xml.Header), body...)
}

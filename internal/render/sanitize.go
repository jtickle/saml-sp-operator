package render

import "strings"

// sanitize.go implements sanitizeComment (D-05): the input-layer "--" strip
// applied to any CRD-derived string before it is routed into an XML
// comment via a struct field tagged xml:",comment".
//
// This is defense-in-depth alongside encoding/xml's own struct-tag comment
// guard (RESEARCH.md Pitfall 4): the ",comment" path already rejects any
// "--" and returns a marshal error, but relying on that error alone means a
// single hostile CRD field aborts the *entire* render — a real availability
// concern (T-06-02). sanitizeComment instead silently collapses "--" at the
// input layer so config generation for the whole SP never aborts over one
// bad comment value.
//
// Never switch comment rendering to the low-level xml.EncodeToken(xml.Comment(...))
// API to "fix" a formatting quirk — that path only rejects the literal "-->"
// sequence, not a bare "--" (RESEARCH.md Pitfall 4), silently weakening this
// guarantee.
//
// As of this plan, shibboleth2.go renders no XML comment anywhere (its own
// package doc comment states the golden fixture is deliberately
// comment-free per RESEARCH.md Pitfall 5), so sanitizeComment has no call
// site in this package today. It exists so a future contributor who adds an
// operator-generated comment (e.g. a "rendered by saml-sp-operator, do not
// edit" marker over a CRD-derived value) has the D-05 guard ready to apply
// before that value reaches a ",comment"-tagged field.
func sanitizeComment(v string) string {
	for strings.Contains(v, "--") {
		v = strings.ReplaceAll(v, "--", "-")
	}
	return v
}

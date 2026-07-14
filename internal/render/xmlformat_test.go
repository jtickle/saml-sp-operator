package render

import (
	"bytes"
	"encoding/xml"
	"testing"
)

// TestMarshalIndentDoesNotSelfClose is an EMPIRICAL check (RESEARCH.md
// Environment Availability note): it marshals a minimal attributes-only,
// no-chardata struct via xml.MarshalIndent and asserts the stdlib output is
// the PAIRED start/end tag form, never the self-closing form. This confirms
// on the real Go 1.26 toolchain that collapseEmptyElements is genuinely
// required — encoding/xml has no code path that ever emits "<Foo/>".
func TestMarshalIndentDoesNotSelfClose(t *testing.T) {
	type emptyAttrsElem struct {
		XMLName xml.Name `xml:"Foo"`
		X       string   `xml:"x,attr"`
	}

	out, err := xml.MarshalIndent(emptyAttrsElem{X: "1"}, "", "    ")
	if err != nil {
		t.Fatalf("xml.MarshalIndent returned error: %v", err)
	}

	want := `<Foo x="1"></Foo>`
	if string(out) != want {
		t.Fatalf("empirical self-closing check: got %q, want paired form %q (if this now fails, stdlib gained self-closing support and collapseEmptyElements may be obsolete)", out, want)
	}
}

// TestCollapseEmptyElements asserts the positive collapse case, the
// chardata negative case, and the mismatched-open/close-name defensive
// branch (RESEARCH.md Pattern 1 / Pitfall 1).
func TestCollapseEmptyElements(t *testing.T) {
	tests := []struct {
		name string
		in   string
		want string
	}{
		{
			name: "empty element with attrs collapses to self-closing",
			in:   `<Foo x="1"></Foo>`,
			want: `<Foo x="1"/>`,
		},
		{
			name: "empty element with multiple attrs collapses",
			in:   `<Handler type="MetadataGenerator" Location="/Metadata" signing="false"></Handler>`,
			want: `<Handler type="MetadataGenerator" Location="/Metadata" signing="false"/>`,
		},
		{
			name: "element with chardata is left untouched",
			in:   `<SSO entityID="https://saml.example.com/entityid">SAML2</SSO>`,
			want: `<SSO entityID="https://saml.example.com/entityid">SAML2</SSO>`,
		},
		{
			name: "mismatched open/close name is left untouched (defensive)",
			in:   `<Foo x="1"></Bar>`,
			want: `<Foo x="1"></Bar>`,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := collapseEmptyElements([]byte(tt.in))
			if !bytes.Equal(got, []byte(tt.want)) {
				t.Fatalf("collapseEmptyElements(%q) = %q, want %q", tt.in, got, tt.want)
			}
		})
	}
}

// TestXMLProlog asserts withXMLProlog prepends xml.Header (which already
// includes the trailing newline) ahead of the marshaled body (RESEARCH.md
// Pitfall 2 — encoding/xml never emits the <?xml ...?> declaration itself).
func TestXMLProlog(t *testing.T) {
	body := []byte(`<Foo x="1"/>`)
	got := withXMLProlog(body)
	want := append([]byte(xml.Header), body...)

	if !bytes.Equal(got, want) {
		t.Fatalf("withXMLProlog(%q) = %q, want %q", body, got, want)
	}

	if !bytes.HasPrefix(got, []byte(`<?xml version="1.0" encoding="UTF-8"?>`)) {
		t.Fatalf("withXMLProlog output missing expected prolog prefix: %q", got)
	}
}

package render

import (
	"os"
	"testing"
)

// goldenAttributeMapAttrs is the fixed sample []AttributeMapping
// TestRenderAttributeMap byte-compares against
// testdata/golden/attribute-map.xml — matching the repo-root
// attribute-map.xml's four-attribute sample set (email, firstName,
// lastName, id->uid). Kept local to this file (rather than reusing
// SampleAppBindings' attributes from fixtures_test.go) so the golden
// fixture's attribute set stays obviously tied to this one byte-compare
// test and doesn't drift if the shared fixtures change shape for
// unrelated reasons (same convention as shibboleth2_test.go's
// goldenShibboleth2SPConfig).
func goldenAttributeMapAttrs() []AttributeMapping {
	return []AttributeMapping{
		{Name: "email", ExportedID: "email"},
		{Name: "firstName", ExportedID: "firstName"},
		{Name: "lastName", ExportedID: "lastName"},
		{Name: "id", ExportedID: "uid"},
	}
}

// TestRenderAttributeMap asserts RenderAttributeMap's output equals
// testdata/golden/attribute-map.xml byte-for-byte (RENDER-03).
func TestRenderAttributeMap(t *testing.T) {
	want, err := os.ReadFile("testdata/golden/attribute-map.xml")
	if err != nil {
		t.Fatalf("reading golden fixture: %v", err)
	}

	got, err := RenderAttributeMap(goldenAttributeMapAttrs())
	if err != nil {
		t.Fatalf("RenderAttributeMap: %v", err)
	}

	if string(got) != string(want) {
		t.Errorf("RenderAttributeMap output does not match golden fixture byte-for-byte\n--- want ---\n%s\n--- got ---\n%s", want, got)
	}
}

// TestAttributeMapOrder asserts the rendered <Attribute> order always
// follows the input slice order, never a Go map range (RENDER-03's
// determinism requirement, same convention as RENDER-04's RequestMap
// ordering).
func TestAttributeMapOrder(t *testing.T) {
	attrs := []AttributeMapping{
		{Name: "zeta", ExportedID: "zeta"},
		{Name: "alpha", ExportedID: "alpha"},
	}

	out, err := RenderAttributeMap(attrs)
	if err != nil {
		t.Fatalf("RenderAttributeMap: %v", err)
	}

	zetaIdx := indexOf(t, string(out), `<Attribute name="zeta" id="zeta"/>`)
	alphaIdx := indexOf(t, string(out), `<Attribute name="alpha" id="alpha"/>`)
	if zetaIdx > alphaIdx {
		t.Errorf("expected input slice order (zeta before alpha) to be preserved, got zeta at %d, alpha at %d:\n%s", zetaIdx, alphaIdx, out)
	}
}

func indexOf(t *testing.T, s, substr string) int {
	t.Helper()
	idx := -1
	for i := 0; i+len(substr) <= len(s); i++ {
		if s[i:i+len(substr)] == substr {
			idx = i
			break
		}
	}
	if idx == -1 {
		t.Fatalf("substring %q not found in:\n%s", substr, s)
	}
	return idx
}

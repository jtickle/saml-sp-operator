package render

import "testing"

// Canonical logical file names for the three rendered artifacts fed to Hash.
// attribute-map.xml reuses the production shibAttributeMapPath constant.
const (
	fixtureShibboleth2Name = "shibboleth2.xml"
	fixtureNginxConfName   = "nginx.conf"
)

// TestConfigHash asserts determinism across repeated calls, change-
// sensitivity to the attribute-map.xml file's bytes (guards D-08 — that
// file is reloadChanges=false and MUST force a pod roll on any change), and
// that the length-prefix scheme disambiguates the ("ab","c")/("a","bc")
// naive-concatenation collision case (RESEARCH.md Pattern 3).
func TestConfigHash(t *testing.T) {
	t.Run("deterministic across repeated calls", func(t *testing.T) {
		files := []ConfigFile{
			{Name: fixtureShibboleth2Name, Bytes: []byte("<SPConfig/>")},
			{Name: fixtureNginxConfName, Bytes: []byte("server {}")},
			{Name: shibAttributeMapPath, Bytes: []byte("<Attributes/>")},
		}

		first := Hash(files)
		for i := range 5 {
			got := Hash(files)
			if got != first {
				t.Fatalf("Hash is non-deterministic: call 1 = %q, call %d = %q", first, i+2, got)
			}
		}
	})

	t.Run("changing attribute-map.xml bytes changes the hash (D-08)", func(t *testing.T) {
		before := []ConfigFile{
			{Name: fixtureShibboleth2Name, Bytes: []byte("<SPConfig/>")},
			{Name: fixtureNginxConfName, Bytes: []byte("server {}")},
			{Name: shibAttributeMapPath, Bytes: []byte("<Attributes/>")},
		}
		after := []ConfigFile{
			{Name: fixtureShibboleth2Name, Bytes: []byte("<SPConfig/>")},
			{Name: fixtureNginxConfName, Bytes: []byte("server {}")},
			{Name: shibAttributeMapPath, Bytes: []byte("<Attributes changed=\"true\"/>")},
		}

		if Hash(before) == Hash(after) {
			t.Fatal("Hash did not change when attribute-map.xml bytes changed — attribute-only changes would silently skip a required pod roll (D-08)")
		}
	})

	t.Run("length-prefixing disambiguates the ab/c vs a/bc split", func(t *testing.T) {
		abC := []ConfigFile{
			{Name: "ab", Bytes: []byte("c")},
		}
		aBC := []ConfigFile{
			{Name: "a", Bytes: []byte("bc")},
		}

		if Hash(abC) == Hash(aBC) {
			t.Fatal("Hash collided on (\"ab\",\"c\") vs (\"a\",\"bc\") — naive concatenation ambiguity, length-prefixing failed to disambiguate")
		}
	})
}

package render

import "testing"

// sanitize_test.go tests sanitizeComment (D-05): the input-layer "--" strip
// applied before any CRD-derived string reaches a struct field tagged
// xml:",comment" (RESEARCH.md Pitfall 4). This is defense-in-depth alongside
// encoding/xml's own struct-tag comment guard (which rejects any "--" and
// aborts the whole render) so a single hostile field is silently sanitized
// rather than aborting config generation for the entire SP (T-06-02).

func TestSanitizeComment(t *testing.T) {
	cases := []struct {
		name string
		in   string
	}{
		{name: "even-run", in: "----"},
		{name: "single-pair-mid-string", in: "a--b"},
		{name: "odd-run", in: "---"},
		{name: "multiple-scattered-pairs", in: "a--b----c"},
		{name: "already-clean", in: "a plain comment with no dashes"},
		{name: "empty", in: ""},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := sanitizeComment(tc.in)
			if containsDoubleDash(got) {
				t.Fatalf("sanitizeComment(%q) = %q, still contains \"--\"", tc.in, got)
			}
		})
	}
}

func TestSanitizeCommentUnchangedWhenAlreadyClean(t *testing.T) {
	in := "a plain comment with no dashes"
	got := sanitizeComment(in)
	if got != in {
		t.Fatalf("sanitizeComment(%q) = %q, want unchanged input", in, got)
	}
}

// containsDoubleDash is a tiny local helper (not strings.Contains directly at
// call sites above) so the assertion failure messages stay uniform across
// every subtest.
func containsDoubleDash(s string) bool {
	for i := 0; i+1 < len(s); i++ {
		if s[i] == '-' && s[i+1] == '-' {
			return true
		}
	}
	return false
}

package render

import "testing"

// clearListTestAttrs is the fixed sample []AttributeMapping TestClearList
// exercises. ExportedID is the attribute id attributemap.go renders into
// attribute-map.xml's <Attribute id=...>, and ClearList prefixes "Variable-"
// onto that same field — so the clear-list names stay consistent with what
// the FastCGI authorizer actually exports for these attributes.
func clearListTestAttrs() []AttributeMapping {
	return []AttributeMapping{
		{Name: "email", ExportedID: "email"},
		{Name: "id", ExportedID: "uid"},
	}
}

// TestClearList asserts RENDER-08's per-attachment-model clear-list value:
// TraefikForwardAuth enumerates every exported header name explicitly
// (Traefik cannot wildcard-strip), NginxAuthRequest returns a Variable-*
// glob instead, and an unrecognized model returns a non-nil error rather
// than silently defaulting to either shape.
func TestClearList(t *testing.T) {
	attrs := clearListTestAttrs()

	t.Run("traefik enumerate", func(t *testing.T) {
		got, err := ClearList(TraefikForwardAuth, attrs)
		if err != nil {
			t.Fatalf("ClearList(TraefikForwardAuth, ...): %v", err)
		}

		if got.Model != TraefikForwardAuth {
			t.Errorf("Model = %q, want %q", got.Model, TraefikForwardAuth)
		}
		if got.Glob != "" {
			t.Errorf("Glob = %q, want empty (Traefik cannot wildcard-strip)", got.Glob)
		}

		wantHeaders := []string{"Variable-REMOTE_USER", "Variable-email", "Variable-uid"}
		if len(got.Headers) != len(wantHeaders) {
			t.Fatalf("Headers = %v, want %v", got.Headers, wantHeaders)
		}
		for i, h := range wantHeaders {
			if got.Headers[i] != h {
				t.Errorf("Headers[%d] = %q, want %q", i, got.Headers[i], h)
			}
		}
	})

	t.Run("nginx glob", func(t *testing.T) {
		got, err := ClearList(NginxAuthRequest, attrs)
		if err != nil {
			t.Fatalf("ClearList(NginxAuthRequest, ...): %v", err)
		}

		if got.Model != NginxAuthRequest {
			t.Errorf("Model = %q, want %q", got.Model, NginxAuthRequest)
		}
		if got.Glob != "Variable-*" {
			t.Errorf("Glob = %q, want %q", got.Glob, "Variable-*")
		}
		if len(got.Headers) != 0 {
			t.Errorf("Headers = %v, want empty (nginx strips by glob, not an enumerated list)", got.Headers)
		}
	})

	t.Run("unknown model", func(t *testing.T) {
		if _, err := ClearList(AttachmentModel("bogus-model"), attrs); err == nil {
			t.Fatal("expected ClearList to reject an unrecognized AttachmentModel, got nil error")
		}
	})
}

package render

import "testing"

// TestSelfURLConsistency exercises DeriveSelfURL (RENDER-02). It asserts the
// two spike fixes that make the self-URL derivation load-bearing rather than
// cosmetic:
//
//   - spike fix N: Port is always explicit, even when the input carries the
//     scheme's default port (bare auto-expansion silently fails OPEN on a
//     non-standard port, so this package never relies on it — see
//     shibboleth2.xml's <Host> comment).
//   - spike fix M: HandlerURL is always fully qualified including the port,
//     never a bare "/Shibboleth.sso" (a relative handlerURL gets
//     port-normalized away by the SP and rejects every handler).
func TestSelfURLConsistency(t *testing.T) {
	t.Run("non-default port is explicit and fully qualified", func(t *testing.T) {
		got, err := DeriveSelfURL("https://app.example.com:30443")
		if err != nil {
			t.Fatalf("DeriveSelfURL returned unexpected error: %v", err)
		}
		want := SelfURL{
			Scheme:     "https",
			Name:       "app.example.com",
			Port:       30443,
			HandlerURL: "https://app.example.com:30443/Shibboleth.sso",
		}
		if got != want {
			t.Fatalf("DeriveSelfURL() = %+v, want %+v", got, want)
		}
	})

	t.Run("default port is still explicit, never relative", func(t *testing.T) {
		got, err := DeriveSelfURL("https://app.example.com")
		if err != nil {
			t.Fatalf("DeriveSelfURL returned unexpected error: %v", err)
		}
		if got.Port != 443 {
			t.Fatalf("DeriveSelfURL() Port = %d, want explicit 443 (spike fix N — never auto-expand)", got.Port)
		}
		want := SelfURL{
			Scheme:     "https",
			Name:       "app.example.com",
			Port:       443,
			HandlerURL: "https://app.example.com:443/Shibboleth.sso",
		}
		if got != want {
			t.Fatalf("DeriveSelfURL() = %+v, want %+v", got, want)
		}
		if got.HandlerURL == "https://app.example.com/Shibboleth.sso" {
			t.Fatalf("DeriveSelfURL() HandlerURL is relative-equivalent (missing port) — spike fix M requires a fully-qualified handlerURL")
		}
	})

	t.Run("non-https external URL is rejected", func(t *testing.T) {
		_, err := DeriveSelfURL("http://app.example.com")
		if err == nil {
			t.Fatal("DeriveSelfURL() error = nil, want non-nil for a non-https external URL")
		}
	})

	t.Run("unparseable external URL is rejected", func(t *testing.T) {
		_, err := DeriveSelfURL("://not a url")
		if err == nil {
			t.Fatal("DeriveSelfURL() error = nil, want non-nil for an unparseable external URL")
		}
	})

	t.Run("empty external URL is rejected", func(t *testing.T) {
		_, err := DeriveSelfURL("")
		if err == nil {
			t.Fatal("DeriveSelfURL() error = nil, want non-nil for an empty external URL")
		}
	})
}

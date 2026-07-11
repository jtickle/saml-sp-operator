package render

import (
	"fmt"
	"os"
	"strings"
	"testing"
)

// TestRenderNginxConf asserts RenderNginxConf's output equals
// testdata/golden/nginx.conf byte-for-byte for SampleSPConfig (RENDER-07),
// and that the rendered external port equals
// DeriveSelfURL(cfg.ExternalURL).Port — the same value shibboleth2.xml
// embeds in handlerURL (spike fixes M/N; a mismatch is the fail-open bug
// D-11 exists to prevent).
func TestRenderNginxConf(t *testing.T) {
	cfg := SampleSPConfig()

	want, err := os.ReadFile("testdata/golden/nginx.conf")
	if err != nil {
		t.Fatalf("reading golden fixture: %v", err)
	}

	got, err := RenderNginxConf(cfg)
	if err != nil {
		t.Fatalf("RenderNginxConf: %v", err)
	}

	if string(got) != string(want) {
		t.Errorf("RenderNginxConf output does not match golden fixture byte-for-byte\n--- want ---\n%s\n--- got ---\n%s", want, got)
	}

	self, err := DeriveSelfURL(cfg.ExternalURL)
	if err != nil {
		t.Fatalf("DeriveSelfURL: %v", err)
	}
	if wantPort := fmt.Sprintf("SERVER_PORT %d;", self.Port); !strings.Contains(string(got), wantPort) {
		t.Errorf("rendered nginx.conf does not carry the external port %d sourced from DeriveSelfURL", self.Port)
	}
}

// TestRenderNginxConfHostileHostname asserts RENDER-10/T-05-01's mitigation:
// a hostname failing validateHostname's allowlist regex makes
// RenderNginxConf return an error before tmpl.Execute runs, rather than
// attempting to interpolate it unescaped into an nginx directive
// (text/template has no auto-escaping of its own).
func TestRenderNginxConfHostileHostname(t *testing.T) {
	cfg := SampleSPConfig()
	// Underscore is not in validateHostname's [a-zA-Z0-9.-] allowlist, but
	// net/url still parses it into a usable Hostname() — an unsanitized
	// nginx directive context would accept far worse (e.g. `;`) unchecked.
	cfg.ExternalURL = "https://ev_il.example.com:30443"

	if _, err := RenderNginxConf(cfg); err == nil {
		t.Fatal("expected RenderNginxConf to reject a hostname failing the allowlist regex, got nil error")
	}
}

package render

import (
	"os"
	"strings"
	"testing"
)

// goldenShibboleth2SPConfig and goldenShibboleth2Winners are the fixed
// sample inputs TestRenderShibboleth2 byte-compares against
// testdata/golden/shibboleth2.xml. Kept local to this file (rather than
// reusing SampleSPConfig/SampleAppBindings from fixtures_test.go) so the
// golden fixture's host/port values stay obviously tied to this one
// byte-compare test and don't drift if the shared fixtures change shape
// for unrelated reasons.
func goldenShibboleth2SPConfig() SPConfig {
	return SPConfig{
		EntityID: "https://app.example.com/shibboleth",
		IdP: IdPConfig{
			MetadataURL: "https://mocksaml.com/api/saml/metadata",
			EntityID:    "https://saml.example.com/entityid",
		},
		CredentialKeyPath:  "credentials/sp-key.pem",
		CredentialCertPath: "credentials/sp-cert.pem",
		RemoteUser:         []string{"email", "uid"},
		Sessions: SessionDefaults{
			LifetimeSeconds: 28800,
			TimeoutSeconds:  3600,
			RelayState:      "ss:mem",
			CheckAddress:    false,
			HandlerSSL:      true,
			CookieProps:     "https",
		},
		ExternalURL: "https://app.example.com:30443",
	}
}

func goldenShibboleth2Winners() []AppBinding {
	return []AppBinding{
		{
			Namespace:      "default",
			Name:           "app",
			UID:            "app-0001",
			Hostname:       "app.example.com",
			Path:           "",
			Scheme:         "https",
			Port:           30443,
			RequireSession: true,
		},
	}
}

// TestRenderShibboleth2 asserts RenderShibboleth2's output equals
// testdata/golden/shibboleth2.xml byte-for-byte (RENDER-01).
func TestRenderShibboleth2(t *testing.T) {
	want, err := os.ReadFile("testdata/golden/shibboleth2.xml")
	if err != nil {
		t.Fatalf("reading golden fixture: %v", err)
	}

	got, err := RenderShibboleth2(goldenShibboleth2SPConfig(), goldenShibboleth2Winners())
	if err != nil {
		t.Fatalf("RenderShibboleth2: %v", err)
	}

	if string(got) != string(want) {
		t.Errorf("RenderShibboleth2 output does not match golden fixture byte-for-byte\n--- want ---\n%s\n--- got ---\n%s", want, got)
	}
}

// TestRequestMapOrdering asserts the RequestMap is aggregated in a
// deterministic order (RENDER-04): every AppBinding in this package has no
// regex-host input, so every group renders as an exact <Host> (never
// <HostRegex>) — and within one hostname, distinct-path winners render as
// nested <Path> children ordered most-specific-path-first.
func TestRequestMapOrdering(t *testing.T) {
	cfg := goldenShibboleth2SPConfig()
	winners := []AppBinding{
		{
			Namespace: "team-a", Name: "api", UID: "aaaa-0001",
			Hostname: "multi.example.com", Path: "/api",
			Scheme: "https", Port: 30443, RequireSession: true,
		},
		{
			Namespace: "team-b", Name: "admin", UID: "bbbb-0002",
			Hostname: "multi.example.com", Path: "/api/admin",
			Scheme: "https", Port: 30443, RequireSession: true,
		},
	}

	tree, err := buildShibboleth2Tree(cfg, winners)
	if err != nil {
		t.Fatalf("buildShibboleth2Tree: %v", err)
	}

	hosts := tree.RequestMapper.RequestMap.Hosts
	if len(hosts) != 1 {
		t.Fatalf("expected 1 grouped Host, got %d: %+v", len(hosts), hosts)
	}

	paths := hosts[0].Paths
	if len(paths) != 2 {
		t.Fatalf("expected 2 nested Path elements, got %d: %+v", len(paths), paths)
	}

	if paths[0].Name != "api/admin" {
		t.Errorf("most-specific path must come first: got paths[0].Name=%q, want %q", paths[0].Name, "api/admin")
	}
	if paths[1].Name != "api" {
		t.Errorf("less-specific path must come second: got paths[1].Name=%q, want %q", paths[1].Name, "api")
	}

	out, err := RenderShibboleth2(cfg, winners)
	if err != nil {
		t.Fatalf("RenderShibboleth2: %v", err)
	}
	if strings.Contains(string(out), "<HostRegex") {
		t.Errorf("rendered output must never contain <HostRegex — this package's AppBinding has no regex-host input:\n%s", out)
	}
	if !strings.Contains(string(out), `<Host name="multi.example.com"`) {
		t.Errorf("rendered output must contain an exact <Host name=\"multi.example.com\" ...> element:\n%s", out)
	}
}

// TestHostSchemePort asserts RENDER-05's negative case: a binding on the
// scheme-default port (443) still renders explicit scheme AND port
// attributes on its <Host> — never relying on bare-hostname auto-expansion
// (spike fix N, T-03-01, ROADMAP crit 3).
func TestHostSchemePort(t *testing.T) {
	cfg := goldenShibboleth2SPConfig()
	winners := []AppBinding{
		{
			Namespace: "default", Name: "app", UID: "app-0002",
			Hostname: "default-port.example.com", Path: "",
			Scheme: "https", Port: 443, RequireSession: true,
		},
	}

	out, err := RenderShibboleth2(cfg, winners)
	if err != nil {
		t.Fatalf("RenderShibboleth2: %v", err)
	}

	want := `<Host name="default-port.example.com" scheme="https" port="443" authType="shibboleth" requireSession="true"/>`
	if !strings.Contains(string(out), want) {
		t.Errorf("default-port (443) Host must still carry explicit scheme+port attributes; want substring:\n%s\ngot:\n%s", want, out)
	}
}

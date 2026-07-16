package render

import (
	"bytes"
	"encoding/xml"
	"io"
	"strings"
	"testing"
)

// inject_test.go is the cross-cutting adversarial proof of RENDER-10 (ROADMAP
// crit 3): every hostile CRD-derived string injected into any render
// entrypoint's free-text fields must never abort the render with a marshal
// error, and the resulting bytes must always re-parse as well-formed XML via
// encoding/xml — proving a single hostile field can never produce
// invalid/FATAL-ing config output (D-05, T-06-01).
//
// This is a mutation-style test: each hostile token is injected into exactly
// one string field of a fresh, otherwise-valid sample input at a time,
// isolating which field (if any) a hostile value could break. It exercises
// the REAL renderers built in plans 03-05 (RenderShibboleth2,
// RenderAttributeMap), not a mock or a hand-simulated marshal step.
//
// Safety is proven in-test via decode round-trips, never via a source-file
// grep for the literal token: encoding/xml auto-escapes every attr/chardata
// value it marshals (RESEARCH.md Code Examples), so a hostile token that
// survives a render+re-parse cycle and reappears as *decoded* attribute or
// chardata text (rather than raw structural markup, or an unparseable
// document) proves it was neutralized as data, not interpreted as XML syntax.

// hostileTokens is the adversarial string table every render entrypoint
// under test must survive without a marshal error, and re-parse
// well-formed for.
var hostileTokens = []struct {
	name string
	// value is the raw hostile string injected into a CRD-derived field.
	value string
	// roundTrips is false for tokens containing characters the XML 1.0 spec
	// disallows outright (control characters other than tab/LF/CR).
	// encoding/xml's EscapeText correctly refuses to emit them literally,
	// substituting the Unicode replacement character (U+FFFD) instead — so
	// decoding back does not reproduce the exact original bytes. That
	// substitution IS the safety property for this token (the invalid bytes
	// never reach the output unmodified), not a round-trip identity, so the
	// token-presence assertion is skipped for these cases; well-formedness
	// and the no-error contract are still asserted.
	roundTrips bool
}{
	{name: "lt", value: "<", roundTrips: true},
	{name: "gt", value: ">", roundTrips: true},
	{name: "amp", value: "&", roundTrips: true},
	{name: "dquote", value: `"`, roundTrips: true},
	{name: "squote", value: "'", roundTrips: true},
	{name: "double-dash", value: "--", roundTrips: true},
	{name: "cdata-close", value: "]]>", roundTrips: true},
	{name: "combo", value: `a<b>c&d"e'f--g]]>h`, roundTrips: true},
	{name: "control-chars", value: "ctrl\x07bell\x1besc\x1funit", roundTrips: false},
}

// baseInjectionCfg, baseInjectionWinners, and baseInjectionAttrs are the
// otherwise-valid sample inputs each injection case mutates exactly one
// field of. Kept local to this file (not fixtures_test.go/goldenShibboleth2*)
// so this adversarial suite never silently drifts if an unrelated golden
// fixture's sample shape changes.
func baseInjectionCfg() SPConfig {
	return SPConfig{
		EntityID: "https://sp.example.com/shibboleth",
		IdP: IdPConfig{
			MetadataURL: "https://idp.example.com/metadata",
			EntityID:    "https://idp.example.com/entityid",
		},
		CredentialKeyPath:  "/run/shibboleth/sp-credentials/tls.key",
		CredentialCertPath: "/run/shibboleth/sp-credentials/tls.crt",
		RemoteUser:         []string{"email", "uid"},
		Sessions: SessionDefaults{
			LifetimeSeconds: 28800,
			TimeoutSeconds:  3600,
			RelayState:      "ss:mem",
			CheckAddress:    false,
			HandlerSSL:      true,
			CookieProps:     "https",
		},
	}
}

func baseInjectionWinners() []AppBinding {
	return []AppBinding{
		{
			Namespace:      "team-a",
			Name:           "app-a",
			UID:            "aaaa-0001",
			Hostname:       "apps.example.com",
			Path:           "/widgets",
			Scheme:         "https",
			Port:           30443,
			RequireSession: true,
		},
	}
}

func baseInjectionAttrs() []AttributeMapping {
	return []AttributeMapping{{Name: "email", ExportedID: "X-Remote-User"}}
}

// injectionField names one string field this suite mutates, across
// SPConfig, AppBinding, and AttributeMapping — the field set the plan names
// explicitly (entityID, hostname, path, attribute name/id, REMOTE_USER,
// credential paths), plus the IdP and session free-text fields that reach
// the same XML tree by the same auto-escaping mechanism.
type injectionField struct {
	name  string
	apply func(cfg *SPConfig, winners []AppBinding, attrs []AttributeMapping, token string)
	// checkIn identifies which renderer's output the mutated field actually
	// reaches, so the token-presence assertion only runs against the output
	// the field feeds — "shib" for RenderShibboleth2 (SPConfig/AppBinding
	// fields), "attr" for RenderAttributeMap (AttributeMapping fields).
	// Both renderers are still invoked and both outputs still asserted
	// well-formed for every case regardless of checkIn — an unaffected
	// output must remain well-formed too, it just carries no copy of this
	// particular field's token to look for.
	checkIn string
}

var injectionFields = []injectionField{
	{"SPConfig.EntityID", func(cfg *SPConfig, _ []AppBinding, _ []AttributeMapping, token string) {
		cfg.EntityID = token
	}, "shib"},
	{"SPConfig.IdP.MetadataURL", func(cfg *SPConfig, _ []AppBinding, _ []AttributeMapping, token string) {
		cfg.IdP.MetadataURL = token
	}, "shib"},
	{"SPConfig.IdP.EntityID", func(cfg *SPConfig, _ []AppBinding, _ []AttributeMapping, token string) {
		cfg.IdP.EntityID = token
	}, "shib"},
	{"SPConfig.CredentialKeyPath", func(cfg *SPConfig, _ []AppBinding, _ []AttributeMapping, token string) {
		cfg.CredentialKeyPath = token
	}, "shib"},
	{"SPConfig.CredentialCertPath", func(cfg *SPConfig, _ []AppBinding, _ []AttributeMapping, token string) {
		cfg.CredentialCertPath = token
	}, "shib"},
	{"SPConfig.RemoteUser", func(cfg *SPConfig, _ []AppBinding, _ []AttributeMapping, token string) {
		cfg.RemoteUser = []string{token, "uid"}
	}, "shib"},
	{"SPConfig.Sessions.RelayState", func(cfg *SPConfig, _ []AppBinding, _ []AttributeMapping, token string) {
		cfg.Sessions.RelayState = token
	}, "shib"},
	{"SPConfig.Sessions.CookieProps", func(cfg *SPConfig, _ []AppBinding, _ []AttributeMapping, token string) {
		cfg.Sessions.CookieProps = token
	}, "shib"},
	{"AppBinding.Hostname", func(_ *SPConfig, winners []AppBinding, _ []AttributeMapping, token string) {
		winners[0].Hostname = token
	}, "shib"},
	{"AppBinding.Path", func(_ *SPConfig, winners []AppBinding, _ []AttributeMapping, token string) {
		winners[0].Path = token
	}, "shib"},
	{"AttributeMapping.Name", func(_ *SPConfig, _ []AppBinding, attrs []AttributeMapping, token string) {
		attrs[0].Name = token
	}, "attr"},
	{"AttributeMapping.ExportedID", func(_ *SPConfig, _ []AppBinding, attrs []AttributeMapping, token string) {
		attrs[0].ExportedID = token
	}, "attr"},
}

// TestInjectionSafety is RENDER-10's end-to-end proof: every hostile token
// injected into every string field above must (a) never cause
// RenderShibboleth2/RenderAttributeMap to return an error, (b) always
// re-parse as well-formed XML, and (c) never appear as unescaped structural
// XML — proven by finding the token embedded in a decoded attribute/chardata
// value, not as raw markup. RenderNginxConf carries no CRD-derived string
// input at all as of RENDER-02 (host-agnostic self-URL): its template
// executes over a single fixed standardHTTPSPort constant, so the prior
// hostile-external-hostname injection case no longer has an applicable
// input to exercise (nginxconf.go).
func TestInjectionSafety(t *testing.T) {
	for _, tok := range hostileTokens {
		t.Run(tok.name, func(t *testing.T) {
			for _, f := range injectionFields {
				t.Run(f.name, func(t *testing.T) {
					cfg := baseInjectionCfg()
					winners := baseInjectionWinners()
					attrs := baseInjectionAttrs()
					f.apply(&cfg, winners, attrs, tok.value)

					shibOut, err := RenderShibboleth2(cfg, winners)
					if err != nil {
						t.Fatalf("RenderShibboleth2 returned an error for hostile %s=%q: %v", f.name, tok.value, err)
					}
					assertWellFormedXML(t, shibOut, "SPConfig")
					if tok.roundTrips && f.checkIn == "shib" {
						assertTokenPresentAsText(t, shibOut, tok.value)
					}

					attrOut, err := RenderAttributeMap(attrs)
					if err != nil {
						t.Fatalf("RenderAttributeMap returned an error for hostile %s=%q: %v", f.name, tok.value, err)
					}
					assertWellFormedXML(t, attrOut, "Attributes")
					if tok.roundTrips && f.checkIn == "attr" {
						assertTokenPresentAsText(t, attrOut, tok.value)
					}
				})
			}
		})
	}
}

// assertWellFormedXML decodes the full token stream of doc via
// encoding/xml.Decoder, failing the test on any decode error (proving the
// document is well-formed — RENDER-10's core guarantee) and asserting the
// document's root element matches wantRoot.
func assertWellFormedXML(t *testing.T, doc []byte, wantRoot string) {
	t.Helper()

	dec := xml.NewDecoder(bytes.NewReader(doc))
	var root string
	for {
		tok, err := dec.Token()
		if err == io.EOF {
			break
		}
		if err != nil {
			t.Fatalf("rendered output does not re-parse as well-formed XML: %v\n--- output ---\n%s", err, doc)
		}
		if se, ok := tok.(xml.StartElement); ok && root == "" {
			root = se.Name.Local
		}
	}

	if root != wantRoot {
		t.Fatalf("rendered output's root element = %q, want %q\n--- output ---\n%s", root, wantRoot, doc)
	}
}

// assertTokenPresentAsText re-decodes doc and asserts token appears
// (verbatim, post-unescape) inside a decoded attribute value or chardata
// span. This is the "never appears as unescaped structural XML" check: had
// the hostile token instead broken out into markup (e.g. a raw "<" opening a
// spurious element), the decoder would not hand back a data token
// containing that literal text — either the document would fail to parse
// (caught by assertWellFormedXML above) or the token would be split across
// element boundaries and never appear intact as one data value.
func assertTokenPresentAsText(t *testing.T, doc []byte, token string) {
	t.Helper()

	dec := xml.NewDecoder(bytes.NewReader(doc))
	for {
		tok, err := dec.Token()
		if err == io.EOF {
			break
		}
		if err != nil {
			t.Fatalf("re-decoding for token-presence check: %v\n--- output ---\n%s", err, doc)
		}

		switch v := tok.(type) {
		case xml.StartElement:
			for _, a := range v.Attr {
				if strings.Contains(a.Value, token) {
					return
				}
			}
		case xml.CharData:
			if strings.Contains(string(v), token) {
				return
			}
		}
	}

	t.Fatalf("hostile token %q never appears intact as a decoded attribute/chardata value — the injection may have been dropped, split, or corrupted rather than safely escaped\n--- output ---\n%s", token, doc)
}

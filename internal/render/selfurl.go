package render

import (
	"fmt"
	"net/url"
)

// SelfURL is the single shared self-URL value consumed by both
// shibboleth2.go (handlerURL, entityID host) and nginxconf.go (SERVER_PORT,
// HTTP_HOST): one computed value, not two independently-typed literals
// (RENDER-02).
type SelfURL struct {
	Scheme string
	Name   string
	Port   int
	// HandlerURL is always fully qualified, including the port even when it
	// is the scheme default (spike fix M — a relative or port-normalized
	// handlerURL makes shibd reject every handler with a protocol mismatch).
	HandlerURL string
}

// schemeDefaultPorts holds the standard port for every scheme this package
// accepts as an external URL. https is the only supported scheme today (the
// operator's SP always sits behind a TLS-terminating gateway); this table
// exists so a future scheme addition has one place to extend rather than a
// scattered literal.
var schemeDefaultPorts = map[string]int{
	"https": 443,
}

// DeriveSelfURL parses an app's external URL into a SelfURL. Port is always
// derived explicitly: when the input URL omits a port, DeriveSelfURL fills
// in the scheme's default port and still emits it on both Port and
// HandlerURL — it never relies on a bare-hostname's implicit standard-port
// auto-expansion (spike fix N). Only https external URLs are accepted; a
// non-https or unparseable input is an error rather than a silent default,
// since a defaulted scheme/port here would produce a RequestMap entry that
// fails open (see shibboleth2.xml's <Host> comment and this plan's D-06/D-07
// context).
func DeriveSelfURL(externalURL string) (SelfURL, error) {
	if externalURL == "" {
		return SelfURL{}, fmt.Errorf("render: external URL is empty")
	}

	u, err := url.Parse(externalURL)
	if err != nil {
		return SelfURL{}, fmt.Errorf("render: external URL %q does not parse: %w", externalURL, err)
	}

	if u.Scheme != "https" {
		return SelfURL{}, fmt.Errorf("render: external URL %q has scheme %q, only \"https\" is supported", externalURL, u.Scheme)
	}

	name := u.Hostname()
	if name == "" {
		return SelfURL{}, fmt.Errorf("render: external URL %q has no host", externalURL)
	}

	port := schemeDefaultPorts[u.Scheme]
	if p := u.Port(); p != "" {
		if _, err := fmt.Sscanf(p, "%d", &port); err != nil {
			return SelfURL{}, fmt.Errorf("render: external URL %q has a non-numeric port %q: %w", externalURL, p, err)
		}
	}

	handlerURL := fmt.Sprintf("%s://%s:%d/Shibboleth.sso", u.Scheme, name, port)

	return SelfURL{
		Scheme:     u.Scheme,
		Name:       name,
		Port:       port,
		HandlerURL: handlerURL,
	}, nil
}

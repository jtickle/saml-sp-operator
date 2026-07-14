package render

import (
	"bytes"
	"fmt"
	"regexp"
	"text/template"
)

// nginxconf.go renders nginx.conf (RENDER-07) — the pure FastCGI->HTTP
// adapter config for the Traefik-ForwardAuth attachment model (nginx.conf,
// repo root). Rendered via text/template (D-04), not encoding/xml: this
// file's structure is line-oriented FastCGI param blocks, not a tree of
// self-closing elements, so the encoding/xml self-closing-tag machinery
// xmlformat.go exists for has no application here.
//
// text/template performs NO auto-escaping (RESEARCH.md Code Examples /
// project PITFALLS.md #11): every CRD-derived string that could reach this
// template MUST pass validateHostname's allowlist BEFORE tmpl.Execute runs
// — reject at render time, never attempt to escape nginx directive syntax
// after the fact (T-05-01). Today the only CRD-derived string in scope is
// the external hostname (SelfURL.Name via DeriveSelfURL); it is validated
// even though this template never interpolates it literally (every host
// reference in the rendered output is nginx's own $host runtime variable,
// matching the repo-root fixture — see nginx.conf:60-66's own explanatory
// comment for why $host, not a literal, is correct here), because a future
// template revision that DOES interpolate a literal host must not silently
// lose this guard.
var nginxConfTemplate = template.Must(template.New("nginx.conf").Parse(nginxConfTemplateSrc))

// nginxConfData is the typed data struct nginxConfTemplate executes over.
// Port is the ONE external-port value this file must agree with
// shibboleth2.xml's handlerURL on (spike fixes M/N) — both files derive it
// from the same DeriveSelfURL(cfg.ExternalURL) call (RENDER-02), never two
// independently-typed literals; a mismatch here is exactly the fail-open
// bug D-11 exists to prevent.
type nginxConfData struct {
	Port int
}

// validHostnameRE is the allowlist every CRD-derived string reaching
// nginxConfTemplate must pass before tmpl.Execute (RESEARCH.md Code
// Examples' exact guard pattern).
var validHostnameRE = regexp.MustCompile(`^[a-zA-Z0-9.-]+$`)

// validateHostname rejects any hostname containing characters that could
// break nginx directive syntax if ever interpolated into this template.
func validateHostname(h string) error {
	if !validHostnameRE.MatchString(h) {
		return fmt.Errorf("render: hostname %q contains characters invalid for an nginx directive context", h)
	}
	return nil
}

// RenderNginxConf renders the full nginx.conf document for cfg (RENDER-07).
// Output equals testdata/golden/nginx.conf byte-for-byte for the sample
// input exercised by TestRenderNginxConf. The external port is sourced from
// DeriveSelfURL(cfg.ExternalURL) — the same value shibboleth2.xml embeds in
// handlerURL — so the two files can never disagree on the external port.
func RenderNginxConf(cfg SPConfig) ([]byte, error) {
	self, err := DeriveSelfURL(cfg.ExternalURL)
	if err != nil {
		return nil, err
	}

	if err := validateHostname(self.Name); err != nil {
		return nil, err
	}

	var buf bytes.Buffer
	if err := nginxConfTemplate.Execute(&buf, nginxConfData{Port: self.Port}); err != nil {
		return nil, fmt.Errorf("render: executing nginx.conf template: %w", err)
	}

	return buf.Bytes(), nil
}

const nginxConfTemplateSrc = `user www-data;
worker_processes auto;
error_log /dev/stderr info;
pid /run/nginx.pid;

events { worker_connections 1024; }

http {
    access_log /dev/stdout;
    default_type application/octet-stream;

    server {
        listen 8080;
        server_name _;

        location = /healthz { access_log off; return 200 "ok\n"; }

        location /Shibboleth.sso {
            fastcgi_param HTTPS       on;
            fastcgi_param SERVER_PORT {{.Port}};
            fastcgi_param SERVER_NAME $host;
            fastcgi_param HTTP_HOST   $host:{{.Port}};
            include fastcgi_params;
            fastcgi_pass unix:/run/shibboleth/shibresponder.sock;
        }

        location /shibboleth-sp {
            alias /usr/share/shibboleth/;
        }

        location = /authcheck {
            fastcgi_param SERVER_NAME    $http_x_forwarded_host;
            fastcgi_param HTTP_HOST      $http_x_forwarded_host;
            fastcgi_param SERVER_PORT    {{.Port}};
            fastcgi_param REQUEST_URI    $http_x_forwarded_uri;
            fastcgi_param DOCUMENT_URI   $http_x_forwarded_uri;
            fastcgi_param REQUEST_METHOD $http_x_forwarded_method;
            fastcgi_param HTTPS          on;
            fastcgi_param HTTP_COOKIE    $http_cookie;

            include fastcgi_params;
            fastcgi_pass unix:/run/shibboleth/shibauthorizer.sock;
        }
    }
}
`

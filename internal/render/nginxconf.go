package render

import (
	"bytes"
	"fmt"
	"text/template"
)

// nginxconf.go renders nginx.conf (RENDER-07) — the pure FastCGI->HTTP
// adapter config for the Traefik-ForwardAuth attachment model (nginx.conf,
// repo root). Rendered via text/template (D-04), not encoding/xml: this
// file's structure is line-oriented FastCGI param blocks, not a tree of
// self-closing elements, so the encoding/xml self-closing-tag machinery
// xmlformat.go exists for has no application here.
//
// One SP now spans multiple app hosts on the standard port (RENDER-02):
// the responder block passes the PER-REQUEST host ($host, no port pin) and
// SERVER_PORT is always the standard 443 — there is no longer a single
// external host/port to derive from SPConfig. Scheme is forced by
// SHIBSP_SERVER_SCHEME=https (pod env), not by this template. See
// shibmultihost_test.go's multiHostNginxConf, this rendering's verified
// target shape.
var nginxConfTemplate = template.Must(template.New("nginx.conf").Parse(nginxConfTemplateSrc))

// standardHTTPSPort is the only SERVER_PORT this template ever emits: with
// a relative handlerURL and no pinned external host, every app is served on
// the standard HTTPS port (RENDER-02).
const standardHTTPSPort = 443

// RenderNginxConf renders the full nginx.conf document for cfg (RENDER-07).
// Output equals testdata/golden/nginx.conf byte-for-byte for the sample
// input exercised by TestRenderNginxConf.
func RenderNginxConf(cfg SPConfig) ([]byte, error) {
	var buf bytes.Buffer
	if err := nginxConfTemplate.Execute(&buf, nginxConfData{Port: standardHTTPSPort}); err != nil {
		return nil, fmt.Errorf("render: executing nginx.conf template: %w", err)
	}

	return buf.Bytes(), nil
}

// nginxConfData is the typed data struct nginxConfTemplate executes over.
type nginxConfData struct {
	Port int
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
            fastcgi_param HTTP_HOST   $host;
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

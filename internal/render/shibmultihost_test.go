//go:build shibdload

package render

// shibmultihost_test.go is a RESEARCH SPIKE (not a committed regression test):
// it verifies the load-bearing assumption behind letting one SPInstance span
// multiple app hosts on standard :443 — that a RELATIVE handlerURL
// ("/Shibboleth.sso") plus a single SHIBSP_SERVER_SCHEME=https override (and NO
// pinned SHIBSP_SERVER_NAME) makes the SP reconstruct the correct PER-HOST ACS
// from the request Host, rather than a single pinned host.
//
// The spike found a relative handlerURL FAILED — but only on the :30443
// NodePort, where the SP normalized the non-standard https port away and the
// handler base stopped prefix-matching the request. On :443 there is no port to
// normalize, so the hypothesis is that relative works and spans hosts.
//
// Reuses the shibdload harness helpers (same package + build tag):
// generateThrowawaySelfSignedCert, shibdLoadIdPMetadata,
// shibMetadataProviderBackingFilePath, cred path consts, success log line.
//
//	go test -tags shibdload ./internal/render/... -run TestMultiHostSelfURL -v

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"net/http"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/testcontainers/testcontainers-go"
	"github.com/testcontainers/testcontainers-go/wait"
)

// multiHostShibboleth2XML is a hand-authored SP config with a RELATIVE
// handlerURL and TWO bare <Host> RequestMap entries (bare = standard :443
// auto-expansion, no scheme/port pin needed off :30443). entityID is a single
// fixed identifier, NOT tied to any serving host. Cred + metadata-backing paths
// match what the test mounts.
func multiHostShibboleth2XML() []byte {
	return []byte(fmt.Sprintf(`<?xml version="1.0" encoding="UTF-8"?>
<SPConfig xmlns="urn:mace:shibboleth:3.0:native:sp:config" clockSkew="180">
    <RequestMapper type="Native">
        <RequestMap>
            <Host name="appa.example.com" authType="shibboleth" requireSession="true"/>
            <Host name="appb.example.com" authType="shibboleth" requireSession="true"/>
        </RequestMap>
    </RequestMapper>
    <ApplicationDefaults entityID="https://sp.example.com/shibboleth" REMOTE_USER="email uid">
        <Sessions lifetime="28800" timeout="3600" relayState="ss:mem"
                  checkAddress="false" handlerSSL="true" cookieProps="https"
                  handlerURL="/Shibboleth.sso">
            <SSO entityID="%s">SAML2</SSO>
            <Logout>SAML2 Local</Logout>
            <Handler type="MetadataGenerator" Location="/Metadata" signing="false"/>
            <Handler type="Status" Location="/Status" acl="127.0.0.1 ::1"/>
            <Handler type="Session" Location="/Session" showAttributeValues="true"/>
        </Sessions>
        <Errors supportContact="root@localhost"/>
        <MetadataProvider type="XML" validate="true" url="%s"
              backingFilePath="%s" maxRefreshDelay="3600"/>
        <AttributeExtractor type="XML" validate="true" reloadChanges="false" path="attribute-map.xml"/>
        <AttributeResolver type="Query" subjectMatch="true"/>
        <AttributeFilter type="XML" validate="true" path="attribute-policy.xml"/>
        <CredentialResolver type="File" key="%s" certificate="%s"/>
    </ApplicationDefaults>
    <SecurityPolicyProvider type="XML" validate="true" path="security-policy.xml"/>
    <ProtocolProvider type="XML" validate="true" reloadChanges="false" path="protocols.xml"/>
</SPConfig>
`, shibdLoadIdPEntityID, shibdLoadMetadataURL, shibMetadataProviderBackingFilePath,
		shibdLoadCredKeyPath, shibdLoadCredCertPath))
}

// multiHostNginxConf is the spike nginx.conf with the :30443 test-artifact pins
// removed: the responder passes the PER-REQUEST host ($host) with NO port, and
// SERVER_PORT is the standard 443. Scheme is forced by SHIBSP_SERVER_SCHEME=https
// (container env), not here.
const multiHostNginxConf = `user www-data;
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
            fastcgi_param SERVER_PORT 443;
            fastcgi_param SERVER_NAME $host;
            fastcgi_param HTTP_HOST   $host;
            include fastcgi_params;
            fastcgi_pass unix:/run/shibboleth/shibresponder.sock;
        }
        location /shibboleth-sp { alias /usr/share/shibboleth/; }
        location = /authcheck {
            fastcgi_param SERVER_NAME    $http_x_forwarded_host;
            fastcgi_param HTTP_HOST      $http_x_forwarded_host;
            fastcgi_param SERVER_PORT    443;
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

// acsLocationRE pulls the AssertionConsumerService Location out of the SP's
// self-generated metadata (/Shibboleth.sso/Metadata).
var acsLocationRE = regexp.MustCompile(`AssertionConsumerService[^>]*Location="([^"]+)"`)

func TestMultiHostSelfURL(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
	defer cancel()

	attributeMapXML, err := RenderAttributeMap(shibdLoadSampleAttributes())
	if err != nil {
		t.Fatalf("RenderAttributeMap: %v", err)
	}
	certPEM, keyPEM := generateThrowawaySelfSignedCert(t)

	req := testcontainers.ContainerRequest{
		Image:        pinnedShibAuthenticatorImage,
		ExposedPorts: []string{"8080/tcp"},
		// The whole point: only the scheme is overridden. No SHIBSP_SERVER_NAME,
		// no SHIBSP_SERVER_PORT — the host must come per-request from HTTP_HOST.
		Env: map[string]string{"SHIBSP_SERVER_SCHEME": "https"},
		Files: []testcontainers.ContainerFile{
			{Reader: bytes.NewReader(multiHostShibboleth2XML()), ContainerFilePath: "/etc/shibboleth/shibboleth2.xml", FileMode: 0o444},
			{Reader: bytes.NewReader(attributeMapXML), ContainerFilePath: "/etc/shibboleth/attribute-map.xml", FileMode: 0o444},
			{Reader: strings.NewReader(multiHostNginxConf), ContainerFilePath: "/etc/nginx/nginx.conf", FileMode: 0o444},
			{Reader: bytes.NewReader(shibdLoadIdPMetadata()), ContainerFilePath: shibMetadataProviderBackingFilePath, FileMode: 0o444},
			{Reader: bytes.NewReader(certPEM), ContainerFilePath: shibdLoadCredCertPath, FileMode: 0o444},
			{Reader: bytes.NewReader(keyPEM), ContainerFilePath: shibdLoadCredKeyPath, FileMode: 0o400},
		},
		WaitingFor: wait.ForLog(shibdLoadSuccessLogLine).WithStartupTimeout(60 * time.Second),
	}

	container, err := testcontainers.GenericContainer(ctx, testcontainers.GenericContainerRequest{
		ContainerRequest: req,
		Started:          true,
	})
	testcontainers.CleanupContainer(t, container)
	if err != nil {
		t.Fatalf("start container: %v", err)
	}

	host, err := container.Host(ctx)
	if err != nil {
		t.Fatalf("container host: %v", err)
	}
	port, err := container.MappedPort(ctx, "8080/tcp")
	if err != nil {
		t.Fatalf("mapped port: %v", err)
	}
	base := fmt.Sprintf("http://%s:%s", host, port.Port())

	// Ask the SP to generate its metadata as if reached on each app host. With a
	// relative handlerURL + per-request HTTP_HOST, the ACS Location in the
	// returned metadata should reflect THAT host.
	for _, appHost := range []string{"appa.example.com", "appb.example.com"} {
		acs := fetchACS(t, ctx, base, appHost)
		t.Logf("Host %-18s -> ACS %q", appHost, acs)
		if !strings.Contains(acs, appHost) {
			t.Errorf("ACS for Host %q did not carry that host: got %q", appHost, acs)
		}
		if !strings.HasPrefix(acs, "https://") {
			t.Errorf("ACS for Host %q is not https (scheme override failed): got %q", appHost, acs)
		}
	}
}

func fetchACS(t *testing.T, ctx context.Context, base, appHost string) string {
	t.Helper()
	r, err := http.NewRequestWithContext(ctx, http.MethodGet, base+"/Shibboleth.sso/Metadata", nil)
	if err != nil {
		t.Fatalf("new request: %v", err)
	}
	r.Host = appHost // the Host header the SP reconstructs its self-URL from
	resp, err := http.DefaultClient.Do(r)
	if err != nil {
		t.Fatalf("GET Metadata as %s: %v", appHost, err)
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("GET Metadata as %s: status %d\nbody:\n%s", appHost, resp.StatusCode, body)
	}
	m := acsLocationRE.FindSubmatch(body)
	if m == nil {
		t.Fatalf("no AssertionConsumerService Location in metadata for %s:\n%s", appHost, body)
	}
	return string(m[1])
}

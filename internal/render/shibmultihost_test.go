//go:build shibdload

package render

// shibmultihost_test.go verifies the load-bearing assumption behind letting
// one SPInstance span multiple app hosts on standard :443: that a RELATIVE
// handlerURL ("/Shibboleth.sso") plus a single SHIBSP_SERVER_SCHEME=https
// override (and NO pinned SHIBSP_SERVER_NAME) makes the SP reconstruct the
// correct PER-HOST ACS from the request Host, rather than a single pinned
// host.
//
// Originally a research spike driving a hand-crafted config string, this
// test now drives THIS PACKAGE's own RenderShibboleth2/RenderNginxConf
// output directly (Task 2, post-RENDER-02) — the same real renderer path
// shibboleth2_test.go's golden byte-compare and shibdload_test.go's
// TestShibdLoad exercise, just with two winning AppBindings on two distinct
// hostnames instead of one.
//
// A prior finding: a relative handlerURL FAILED on the :30443 NodePort,
// where the SP normalized the non-standard https port away and the handler
// base stopped prefix-matching the request. On :443 there is no port to
// normalize, so this test's winners deliberately use Port: 443.
//
// Reuses the shibdload harness helpers (same package + build tag):
// generateThrowawaySelfSignedCert, shibdLoadIdPMetadata,
// shibMetadataProviderBackingFilePath, shibdLoadSampleSPConfig,
// shibdLoadSampleAttributes, cred path consts, success log line.
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

// multiHostWinners returns two AppBindings on distinct hostnames, both on
// the standard HTTPS port (443, per the finding above) with a session
// required — the input RenderShibboleth2 aggregates into two bare <Host>
// RequestMap entries (RENDER-04).
func multiHostWinners() []AppBinding {
	return []AppBinding{
		{
			Namespace:      "team-a",
			Name:           "appa",
			UID:            "appa-0001",
			Hostname:       "appa.example.com",
			Scheme:         "https",
			Port:           443,
			RequireSession: true,
		},
		{
			Namespace:      "team-b",
			Name:           "appb",
			UID:            "appb-0002",
			Hostname:       "appb.example.com",
			Scheme:         "https",
			Port:           443,
			RequireSession: true,
		},
	}
}

// acsLocationRE pulls the AssertionConsumerService Location out of the SP's
// self-generated metadata (/Shibboleth.sso/Metadata).
var acsLocationRE = regexp.MustCompile(`AssertionConsumerService[^>]*Location="([^"]+)"`)

func TestMultiHostSelfURL(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
	defer cancel()

	cfg := shibdLoadSampleSPConfig()
	winners := multiHostWinners()

	shibboleth2XML, err := RenderShibboleth2(cfg, winners)
	if err != nil {
		t.Fatalf("RenderShibboleth2: %v", err)
	}
	attributeMapXML, err := RenderAttributeMap(shibdLoadSampleAttributes())
	if err != nil {
		t.Fatalf("RenderAttributeMap: %v", err)
	}
	nginxConf, err := RenderNginxConf(cfg)
	if err != nil {
		t.Fatalf("RenderNginxConf: %v", err)
	}
	certPEM, keyPEM := generateThrowawaySelfSignedCert(t)

	req := testcontainers.ContainerRequest{
		Image:        pinnedShibAuthenticatorImage,
		ExposedPorts: []string{"8080/tcp"},
		// The whole point: only the scheme is overridden. No SHIBSP_SERVER_NAME,
		// no SHIBSP_SERVER_PORT — the host must come per-request from HTTP_HOST.
		Env: map[string]string{"SHIBSP_SERVER_SCHEME": "https"},
		Files: []testcontainers.ContainerFile{
			{Reader: bytes.NewReader(shibboleth2XML), ContainerFilePath: "/etc/shibboleth/shibboleth2.xml", FileMode: 0o444},
			{Reader: bytes.NewReader(attributeMapXML), ContainerFilePath: "/etc/shibboleth/attribute-map.xml", FileMode: 0o444},
			{Reader: bytes.NewReader(nginxConf), ContainerFilePath: "/etc/nginx/nginx.conf", FileMode: 0o444},
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
	for _, w := range winners {
		appHost := w.Hostname
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

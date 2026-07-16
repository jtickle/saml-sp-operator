//go:build shibdload

package render

// shibdload_test.go is the build-time net (1 of 3, D-11) of the
// fail-safe-rollout guarantee: it mounts THIS package's own rendered
// shibboleth2.xml + attribute-map.xml + nginx.conf into a real,
// containerized shibd and asserts shibd loads them with no FATAL —
// closing the gap a golden byte-compare alone cannot close (ROADMAP
// success criterion #1).
//
// Gated behind the "shibdload" build tag (this file's first line) so
// plain `go build`/`go vet`/`go test ./internal/render/...` stays
// hermetic and Docker-free (D-01/D-02's k8s-free/container-dep-free
// boundary is verified separately by `go list -deps` on the non-test
// package, see the plan's verify step). Run explicitly via:
//
//	go test -tags shibdload ./internal/render/... -run TestShibdLoad -v
//
// The load-harness SHAPE (ContainerRequest.Files bind-mount into a real
// Shibboleth SP container image) is the only thing borrowed from a prior
// internal spike; no content, product name, or thread/issue reference is
// carried over (this is a public repo — RESEARCH.md Pitfall 7).

import (
	"bytes"
	"context"
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"io"
	"math/big"
	"strings"
	"testing"
	"time"

	"github.com/testcontainers/testcontainers-go"
	"github.com/testcontainers/testcontainers-go/wait"
)

// pinnedShibAuthenticatorImage is the shib-authenticator image this load
// test mounts the render package's own output into. It is pinned by
// immutable sha256 digest (NOT the floating "spike"/"main" tag) so the
// tested artifact can never silently change out from under this test —
// RESEARCH.md Pitfall 8. The digest was confirmed anonymously pullable
// (GHCR package visibility Public) per this plan's Task 1 checkpoint.
const pinnedShibAuthenticatorImage = "ghcr.io/jtickle/saml-sp-operator/shib-authenticator@sha256:0e33ee7fea4524cb3caa8744b22f05a80703d22444ef198368484dc523f41319"

// shibdLoadSuccessLogLine is shibd's own log line on a clean startup
// (observed directly against pinnedShibAuthenticatorImage: a config-parse
// failure never reaches this line — shibd instead exits or logs FATAL
// before it, which is exactly the fail-safe gap this test closes).
const shibdLoadSuccessLogLine = "Shibboleth initialization complete."

// shibdLoadIdPEntityID and shibdLoadMetadataURL describe a throwaway,
// never-resolvable IdP (RFC 2606 reserved ".invalid" TLD) so the
// MetadataProvider's remote fetch fails fast and deterministically, every
// run, with no real network dependency — shibd then falls back to the
// static backing-file metadata this test mounts at the render package's
// own shibMetadataProviderBackingFilePath, which is the actual behavior
// under test (a real containerized shibd successfully loading the
// package's rendered config), not live SAML federation with a real IdP.
const (
	shibdLoadIdPEntityID  = "https://idp.invalid.example/idp/shibboleth"
	shibdLoadMetadataURL  = "https://idp.invalid.example/metadata"
	shibdLoadCredKeyPath  = "/run/shibboleth/sp-credentials/tls.key"
	shibdLoadCredCertPath = "/run/shibboleth/sp-credentials/tls.crt"
)

// shibdLoadSampleSPConfig returns the SPConfig this test renders and
// mounts. It is a load-test-local fixture (not fixtures_test.go's shared
// SampleSPConfig) because the MetadataURL/IdP.EntityID/credential paths
// here are deliberately shaped for a hermetic containerized load test
// (fast-failing .invalid metadata host, credential paths matching this
// file's mounted files), not for the golden byte-compare fixtures plans
// 03/04/05 already lock.
func shibdLoadSampleSPConfig() SPConfig {
	return SPConfig{
		EntityID: "https://sp.example.com/shibboleth",
		IdP: IdPConfig{
			MetadataURL: shibdLoadMetadataURL,
			EntityID:    shibdLoadIdPEntityID,
		},
		CredentialKeyPath:  shibdLoadCredKeyPath,
		CredentialCertPath: shibdLoadCredCertPath,
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

func shibdLoadSampleWinners() []AppBinding {
	return []AppBinding{
		{
			Namespace:      "load-test",
			Name:           "app",
			UID:            "shibdload-0001",
			Hostname:       "sp.example.com",
			Path:           "/",
			Scheme:         "https",
			Port:           8080,
			RequireSession: true,
			Attributes: []AttributeMapping{
				{Name: "email", ExportedID: "email"},
			},
		},
	}
}

func shibdLoadSampleAttributes() []AttributeMapping {
	return []AttributeMapping{
		{Name: "email", ExportedID: "email"},
		{Name: "uid", ExportedID: "uid"},
	}
}

// shibdLoadIdPMetadata is a minimal, hand-authored (not copied from any
// third-party or product source) SAML 2.0 IdP EntityDescriptor. Its only
// job is to give shibd's XML MetadataProvider a structurally valid backing
// file to fall back to once the (deliberately unreachable) remote URL
// fetch fails — see the shibdLoadIdPEntityID/MetadataURL doc comment.
func shibdLoadIdPMetadata() []byte {
	return []byte(`<EntityDescriptor
    xmlns="urn:oasis:names:tc:SAML:2.0:metadata"
    entityID="` + shibdLoadIdPEntityID + `">
    <IDPSSODescriptor protocolSupportEnumeration="urn:oasis:names:tc:SAML:2.0:protocol">
        <SingleSignOnService Binding="urn:oasis:names:tc:SAML:2.0:bindings:HTTP-Redirect"
            Location="https://idp.invalid.example/idp/profile/SAML2/Redirect/SSO"/>
    </IDPSSODescriptor>
</EntityDescriptor>
`)
}

// generateThrowawaySelfSignedCert generates a fresh, throwaway self-signed
// RSA keypair + certificate at test time — never a reused or committed
// private key (this is a public repo; T-01-SC-adjacent hygiene). The
// keypair exists only to satisfy shibd's CredentialResolver file-load
// requirement and is discarded with the container at test end.
func generateThrowawaySelfSignedCert(t *testing.T) (certPEM, keyPEM []byte) {
	t.Helper()

	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatalf("generate throwaway RSA key: %v", err)
	}

	serial, err := rand.Int(rand.Reader, new(big.Int).Lsh(big.NewInt(1), 128))
	if err != nil {
		t.Fatalf("generate throwaway cert serial: %v", err)
	}

	tmpl := &x509.Certificate{
		SerialNumber:          serial,
		Subject:               pkix.Name{CommonName: "sp.example.com"},
		NotBefore:             time.Now().Add(-time.Hour),
		NotAfter:              time.Now().Add(time.Hour),
		KeyUsage:              x509.KeyUsageDigitalSignature | x509.KeyUsageKeyEncipherment,
		ExtKeyUsage:           []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
		BasicConstraintsValid: true,
	}

	der, err := x509.CreateCertificate(rand.Reader, tmpl, tmpl, &key.PublicKey, key)
	if err != nil {
		t.Fatalf("create throwaway self-signed cert: %v", err)
	}

	certPEM = pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der})
	keyPEM = pem.EncodeToMemory(&pem.Block{Type: "RSA PRIVATE KEY", Bytes: x509.MarshalPKCS1PrivateKey(key)})
	return certPEM, keyPEM
}

// TestShibdLoad renders this package's own shibboleth2.xml,
// attribute-map.xml, and nginx.conf, mounts them (plus a throwaway
// sp-credentials keypair and a static IdP metadata backing file) into a
// real, pinned shib-authenticator container, and asserts shibd reaches
// its successful-startup log line with no FATAL anywhere in the
// container's combined log output — the ROADMAP success-criterion-#1 net
// a golden byte-compare cannot provide on its own.
func TestShibdLoad(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()

	cfg := shibdLoadSampleSPConfig()

	shibboleth2XML, err := RenderShibboleth2(cfg, shibdLoadSampleWinners())
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
		Files: []testcontainers.ContainerFile{
			{
				Reader:            bytes.NewReader(shibboleth2XML),
				ContainerFilePath: "/etc/shibboleth/shibboleth2.xml",
				FileMode:          0o444,
			},
			{
				Reader:            bytes.NewReader(attributeMapXML),
				ContainerFilePath: "/etc/shibboleth/attribute-map.xml",
				FileMode:          0o444,
			},
			{
				Reader:            bytes.NewReader(nginxConf),
				ContainerFilePath: "/etc/nginx/nginx.conf",
				FileMode:          0o444,
			},
			{
				// Matches shibMetadataProviderBackingFilePath
				// (shibboleth2.go) — the backing file the SP's
				// MetadataProvider falls back to once the deliberately
				// unreachable shibdLoadMetadataURL fetch fails.
				Reader:            bytes.NewReader(shibdLoadIdPMetadata()),
				ContainerFilePath: shibMetadataProviderBackingFilePath,
				FileMode:          0o444,
			},
			{
				Reader:            bytes.NewReader(certPEM),
				ContainerFilePath: shibdLoadCredCertPath,
				FileMode:          0o444,
			},
			{
				Reader:            bytes.NewReader(keyPEM),
				ContainerFilePath: shibdLoadCredKeyPath,
				FileMode:          0o400,
			},
		},
		WaitingFor: wait.ForLog(shibdLoadSuccessLogLine).WithStartupTimeout(60 * time.Second),
	}

	container, err := testcontainers.GenericContainer(ctx, testcontainers.GenericContainerRequest{
		ContainerRequest: req,
		Started:          true,
	})
	testcontainers.CleanupContainer(t, container)
	if err != nil {
		t.Fatalf("start shib-authenticator container: %v", err)
	}

	logsReader, err := container.Logs(ctx)
	if err != nil {
		t.Fatalf("fetch container logs: %v", err)
	}
	defer logsReader.Close()

	logs, err := io.ReadAll(logsReader)
	if err != nil {
		t.Fatalf("read container logs: %v", err)
	}

	if strings.Contains(string(logs), "FATAL") {
		t.Fatalf("shibd load produced a FATAL log line — rendered config did not load cleanly:\n%s", logs)
	}
	if !strings.Contains(string(logs), shibdLoadSuccessLogLine) {
		t.Fatalf("shibd never reached its successful-startup log line %q:\n%s", shibdLoadSuccessLogLine, logs)
	}
}

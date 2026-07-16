//go:build shibdload

package render

// shibreadiness_test.go closes Task 9's "confirm the container contract"
// item: it boots the pinned shib-authenticator image with this package's
// own rendered config, mounted at the SAME final paths the operator
// Deployment uses (internal/controller/spinstance_objects.go's
// shib-config ConfigMap subPaths, sp-credentials Secret mount, and
// shib-run emptyDir), sets the same SHIBSP_SERVER_SCHEME=https env var
// the Deployment sets, waits for shibd to finish loading, then execs the
// EXACT readiness probe command
// (curl -fsS http://localhost:8080/Shibboleth.sso/Status) inside the
// container and asserts it exits 0 — proving the Deployment's readiness
// probe actually succeeds against a healthy shibd, not just that shibd
// starts without a FATAL log line (TestShibdLoad's narrower claim).
//
// Reuses the shibdload harness helpers (same package + build tag):
// pinnedShibAuthenticatorImage, generateThrowawaySelfSignedCert,
// shibdLoadIdPMetadata, shibMetadataProviderBackingFilePath, cred path
// consts, shibdLoadSampleSPConfig, shibdLoadSampleWinners,
// shibdLoadSampleAttributes, shibdLoadSuccessLogLine.
//
//	go test -tags shibdload ./internal/render/... -run TestReadinessProbeStatus -v

import (
	"bytes"
	"context"
	"io"
	"testing"
	"time"

	"github.com/testcontainers/testcontainers-go"
	"github.com/testcontainers/testcontainers-go/wait"
)

// TestReadinessProbeStatus renders this package's own shibboleth2.xml,
// attribute-map.xml, and nginx.conf, mounts them (plus a throwaway
// sp-credentials keypair and a static IdP metadata backing file) into a
// real, pinned shib-authenticator container at the operator Deployment's
// mount paths, and then runs the Deployment's exact readiness probe
// command inside the container — asserting it exits 0 against a healthy
// shibd (SPI-03).
func TestReadinessProbeStatus(t *testing.T) {
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
		// Matches the Deployment's container Env exactly
		// (spinstance_objects.go's reconcileDeployment).
		Env: map[string]string{"SHIBSP_SERVER_SCHEME": "https"},
		Files: []testcontainers.ContainerFile{
			{
				// Matches the shib-config ConfigMap's shibboleth2.xml
				// subPath mount.
				Reader:            bytes.NewReader(shibboleth2XML),
				ContainerFilePath: "/etc/shibboleth/shibboleth2.xml",
				FileMode:          0o444,
			},
			{
				// Matches the shib-config ConfigMap's attribute-map.xml
				// subPath mount.
				Reader:            bytes.NewReader(attributeMapXML),
				ContainerFilePath: "/etc/shibboleth/attribute-map.xml",
				FileMode:          0o444,
			},
			{
				// Matches the shib-config ConfigMap's nginx.conf subPath
				// mount.
				Reader:            bytes.NewReader(nginxConf),
				ContainerFilePath: "/etc/nginx/nginx.conf",
				FileMode:          0o444,
			},
			{
				// Matches the shib-run emptyDir mount
				// (shibMetadataProviderBackingFilePath lives under
				// /run/shibboleth, same mount as the Deployment's
				// shib-run volume).
				Reader:            bytes.NewReader(shibdLoadIdPMetadata()),
				ContainerFilePath: shibMetadataProviderBackingFilePath,
				FileMode:          0o444,
			},
			{
				// Matches the sp-credentials Secret mount at
				// /run/shibboleth/sp-credentials.
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

	// The exact command the Deployment's readiness probe execs
	// (spinstance_objects.go's reconcileDeployment ReadinessProbe.Exec).
	exitCode, outReader, err := container.Exec(ctx, []string{"curl", "-fsS", "http://localhost:8080/Shibboleth.sso/Status"})
	if err != nil {
		t.Fatalf("exec readiness probe command: %v", err)
	}

	var out bytes.Buffer
	if outReader != nil {
		if _, readErr := io.Copy(&out, outReader); readErr != nil {
			t.Fatalf("read readiness probe output: %v", readErr)
		}
	}

	if exitCode != 0 {
		t.Fatalf("readiness probe command exited %d, want 0\noutput:\n%s", exitCode, out.String())
	}
}

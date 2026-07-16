package render

import (
	"fmt"
	"os"
	"strings"
	"testing"
)

// TestRenderNginxConf asserts RenderNginxConf's output equals
// testdata/golden/nginx.conf byte-for-byte for SampleSPConfig (RENDER-07),
// and that the rendered port is always the standard HTTPS port (RENDER-02
// — one SP now spans multiple app hosts, so no single external port is
// derived from SPConfig anymore; this is the same value shibboleth2.xml's
// relative handlerURL implies).
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

	if wantPort := fmt.Sprintf("SERVER_PORT %d;", standardHTTPSPort); !strings.Contains(string(got), wantPort) {
		t.Errorf("rendered nginx.conf does not carry the standard HTTPS port %d", standardHTTPSPort)
	}
}

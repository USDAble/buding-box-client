package server

import (
	"net/http"
	"os"
	"strings"
	"testing"
)

func TestArtifactPreviewBridgePreservesSourceAndIsolation(t *testing.T) {
	const source = `<html><body style="width:900px;height:1350px;overflow:hidden">poster</body></html>`
	f := newOriginFixture(t, source)
	response := f.originGet(t, f.token(t), "/")
	if response.Code != http.StatusOK || !strings.Contains(response.Body.String(), "octo:artifact-layout") {
		t.Fatalf("missing preview layout bridge: status=%d", response.Code)
	}
	if strings.Count(response.Body.String(), "octo:artifact-layout") != 1 {
		t.Fatal("layout bridge was injected more than once")
	}
	if !strings.Contains(response.Header().Get("Content-Security-Policy"), "frame-ancestors") {
		t.Fatal("preview lost origin restrictions")
	}
	stored, err := os.ReadFile(f.entry)
	if err != nil || string(stored) != source {
		t.Fatalf("preview changed the deliverable: %v", err)
	}
}

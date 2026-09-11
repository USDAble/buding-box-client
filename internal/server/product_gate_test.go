package server

import (
	"encoding/json"
	"net/http"
	"testing"
)

// testWindowToken is a 64-hex stand-in for the shell's per-launch token.
const testWindowToken = "3f2a1b0c9d8e7f605142332415061728293a3b3c4d4e4f505152535455565758"

// testProductHandler answers a mounted product route. A mounted route is a
// stand-in for this fork's own routes, registered through the same seam the fork
// uses (Config.MountAPI) so this package never imports internal/productruntime.
func testProductHandler(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, map[string]string{"ok": "true"})
}

// mountTestProduct registers a product route through the fork's seam.
func mountTestProduct(api func(pattern string, h http.HandlerFunc)) {
	api("GET /api/product/test-state", testProductHandler)
}

// The window token is the product gate's window identity (需求基线 E5, P3 §3.2).
// These tests pin the server half, including its scope: the gate covers the
// routes mounted through Config.MountAPI — this fork's own product API — and
// nothing else.

// TestProductGateRejectsMountedRoutesWithoutTheWindowToken is the gate's
// contract: when a token is configured, a product route must present it, and
// only that exact value opens the route.
func TestProductGateRejectsMountedRoutesWithoutTheWindowToken(t *testing.T) {
	srv := mustServer(t, Config{
		AccessKey:   testAccessKey,
		WindowToken: testWindowToken,
		MountAPI:    mountTestProduct,
	})

	cases := []struct {
		name string
		hdr  map[string]string
		want int
	}{
		{"no header", nil, http.StatusForbidden},
		{"wrong token", map[string]string{windowTokenHeader: "not-the-token"}, http.StatusForbidden},
		{"empty value", map[string]string{windowTokenHeader: ""}, http.StatusForbidden},
		{"correct token", map[string]string{windowTokenHeader: testWindowToken}, http.StatusOK},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			w := authRequest(t, srv, http.MethodGet, "http://127.0.0.1:8080/api/product/test-state", "127.0.0.1:50000", tc.hdr)
			if w.Code != tc.want {
				t.Fatalf("status = %d, want %d (body: %.200s)", w.Code, tc.want, w.Body.String())
			}
		})
	}
}

// TestProductGateBodyNamesTheGate pins the wire value the frontend keys on: a
// 403 whose body does not say product_gate would leave the UI on a dead screen
// instead of routing to the login gate (web/src/lib/api.ts:56).
func TestProductGateBodyNamesTheGate(t *testing.T) {
	srv := mustServer(t, Config{
		AccessKey:   testAccessKey,
		WindowToken: testWindowToken,
		MountAPI:    mountTestProduct,
	})

	w := authRequest(t, srv, http.MethodGet, "http://127.0.0.1:8080/api/product/test-state", "127.0.0.1:50000", nil)
	if w.Code != http.StatusForbidden {
		t.Fatalf("status = %d, want 403 (body: %.200s)", w.Code, w.Body.String())
	}
	var body map[string]any
	if err := json.Unmarshal(w.Body.Bytes(), &body); err != nil {
		t.Fatalf("403 body is not JSON (%v): %.200s", err, w.Body.String())
	}
	if body["error"] != "product_gate" {
		t.Fatalf(`body["error"] = %v, want "product_gate"`, body["error"])
	}
}

// TestProductGateLeavesUpstreamAPIRoutesAlone is the scope pin, and it is a
// regression test before it is a spec test: a gate over all of /api/* shipped
// once and broke the chat's attachment thumbnails and artifact image previews,
// because those load as bare <img src="/api/…"> subresources that cannot carry a
// custom header. Every route here is one the browser fetches that way, or one an
// out-of-window local process legitimately drives:
//
//   - GET /api/uploads/{name}            chat image thumbnails (ChatView.svelte)
//   - GET /api/sessions/{id}/artifacts   artifact images, host document
//   - GET /api/sessions                  the CLI's loopback path (octo serve)
//
// The gate must not answer product_gate for any of them, token configured or not.
func TestProductGateLeavesUpstreamAPIRoutesAlone(t *testing.T) {
	srv := mustServer(t, Config{
		AccessKey:   testAccessKey,
		WindowToken: testWindowToken,
		MountAPI:    mountTestProduct,
	})

	for _, target := range []string{
		"http://127.0.0.1:8080/api/uploads/thumb.png",
		"http://127.0.0.1:8080/api/sessions/s1/artifacts?path=shot.png",
		"http://127.0.0.1:8080/api/sessions",
	} {
		t.Run(target, func(t *testing.T) {
			w := authRequest(t, srv, http.MethodGet, target, "127.0.0.1:50000", nil)
			if w.Code == http.StatusForbidden {
				t.Fatalf("%s was gated: status %d (body: %.200s)", target, w.Code, w.Body.String())
			}
		})
	}
}

// TestProductGateIsOffWithoutAConfiguredWindowToken pins the CLI path: octo
// serve configures no token, so even a mounted route must keep behaving as it
// would upstream. Merges both the seam and the gate.
func TestProductGateIsOffWithoutAConfiguredWindowToken(t *testing.T) {
	srv := mustServer(t, Config{
		AccessKey: testAccessKey,
		MountAPI:  mountTestProduct,
	})

	w := authRequest(t, srv, http.MethodGet, "http://127.0.0.1:8080/api/product/test-state", "127.0.0.1:50000", nil)
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200 with no token configured (body: %.200s)", w.Code, w.Body.String())
	}
}

// TestProductGateLeavesTheWebSocketRouteAlone guards the gate's scope at the
// registrar boundary: /ws is registered through the upstream registrar, so it is
// structurally out of the gate — there is no path rule to inherit by accident.
// A plain GET without an Upgrade handshake is not 403 here regardless of what
// handleWS answers; what this rejects is the gate swallowing it.
func TestProductGateLeavesTheWebSocketRouteAlone(t *testing.T) {
	srv := mustServer(t, Config{AccessKey: testAccessKey, WindowToken: testWindowToken})

	w := authRequest(t, srv, http.MethodGet, "http://127.0.0.1:8080/ws", "127.0.0.1:50000", nil)
	if w.Code == http.StatusForbidden {
		t.Fatalf("/ws was gated: status %d (body: %.200s)", w.Code, w.Body.String())
	}
}

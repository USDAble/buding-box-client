package server

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestHandleTunnelPairing(t *testing.T) {
	s := &Server{}

	// Off by default: enabled=false, no material.
	rec := httptest.NewRecorder()
	s.handleTunnelPairing(rec, httptest.NewRequest(http.MethodGet, "/api/tunnel/pairing", nil))
	var off map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &off); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if off["enabled"] != false {
		t.Errorf("enabled = %v, want false", off["enabled"])
	}
	if _, ok := off["pair_url"]; ok {
		t.Error("pair_url should be absent when the tunnel is off")
	}

	// Published: the material comes back verbatim.
	s.SetTunnelPairing(&TunnelPairing{
		PairURL:  "octo-pair://v1?tok=abc",
		Relay:    "wss://relay.octo.dev",
		TunnelID: "deadbeef",
	})
	rec = httptest.NewRecorder()
	s.handleTunnelPairing(rec, httptest.NewRequest(http.MethodGet, "/api/tunnel/pairing", nil))
	var on map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &on); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if on["enabled"] != true {
		t.Errorf("enabled = %v, want true", on["enabled"])
	}
	if on["pair_url"] != "octo-pair://v1?tok=abc" {
		t.Errorf("pair_url = %v", on["pair_url"])
	}
	if on["tunnel_id"] != "deadbeef" {
		t.Errorf("tunnel_id = %v", on["tunnel_id"])
	}
}

// OCTO-FORK: the desktop QR carries a one-time remote-access token and may
// only be read by its local window; the CLI's ungated route stays covered above.
func TestHandleTunnelPairingDesktopGate(t *testing.T) {
	s := &Server{cfg: Config{WindowToken: "window-token"}}
	s.SetTunnelPairing(&TunnelPairing{PairURL: "octo-pair://v1?tok=secret"})
	for _, tc := range []struct {
		name, remote, token string
		want                int
	}{
		{"window", "127.0.0.1:3000", "window-token", http.StatusOK},
		{"other browser", "127.0.0.1:3000", "", http.StatusForbidden},
		{"remote phone", "127.0.0.1:3000", "window-token", http.StatusForbidden},
	} {
		t.Run(tc.name, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodGet, "http://127.0.0.1:8088/api/tunnel/pairing", nil)
			req.RemoteAddr = tc.remote
			if tc.token != "" {
				req.Header.Set(windowTokenHeader, tc.token)
			}
			if tc.name == "remote phone" {
				req.Header.Set(HeaderForwarded, "relay")
			}
			rec := httptest.NewRecorder()
			s.handleTunnelPairing(rec, req)
			if rec.Code != tc.want {
				t.Fatalf("status = %d, want %d", rec.Code, tc.want)
			}
		})
	}
}

package server

import "net/http"

// TunnelPairing is the material a phone needs to pair with this host over the
// managed tunnel: the deep-link URL a QR encodes, plus display fields.
// OCTO-FORK: CLI serve and the desktop's in-process tunnel share this display
// contract, while the server remains unaware of Noise and relay mechanics.
type TunnelPairing struct {
	PairURL  string `json:"pair_url"`
	Relay    string `json:"relay"`
	TunnelID string `json:"tunnel_id"`
}

// SetTunnelPairing publishes the current pairing material, or clears it with
// nil. Safe for concurrent use.
func (s *Server) SetTunnelPairing(p *TunnelPairing) {
	s.tunnelPairing.Store(p)
}

// handleTunnelPairing returns the pairing material for the web UI. When the
// managed tunnel is off, enabled is false and there is nothing to render.
func (s *Server) handleTunnelPairing(w http.ResponseWriter, r *http.Request) {
	// OCTO-FORK: once the desktop can start a tunnel, its one-time pairing
	// token must be visible only to that local window, never to a relayed phone
	// or an unrelated browser. Plain CLI serve keeps its original behavior.
	if s.cfg.WindowToken != "" && (!isLocalRequest(r) || !s.windowAllowed(r)) {
		writeJSON(w, http.StatusForbidden, map[string]string{"error": "product_gate"})
		return
	}
	p := s.tunnelPairing.Load()
	if p == nil {
		writeJSON(w, http.StatusOK, map[string]any{"enabled": false})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"enabled":   true,
		"pair_url":  p.PairURL,
		"relay":     p.Relay,
		"tunnel_id": p.TunnelID,
	})
}

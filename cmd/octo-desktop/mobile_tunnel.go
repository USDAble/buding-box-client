package main

import (
	"context"
	"encoding/json"
	"log/slog"
	"net/http"
	"sync"

	"github.com/open-octo/octo-agent/internal/server"
	"github.com/open-octo/octo-agent/internal/tunnel"
)

// OCTO-FORK: the desktop hub owns the phone tunnel in-process. Starting a
// second `serve` would contend for its port and expose a different server.
type desktopTunnel struct {
	mu       sync.Mutex
	srv      *server.Server
	cancel   context.CancelFunc
	done     chan struct{}
	stopDone chan struct{}
	state    string
	error    string
	pairing  server.TunnelPairing
}

type desktopTunnelStatus struct {
	State string `json:"state"`
	Error string `json:"error,omitempty"`
}

func (d *desktopTunnel) bind(srv *server.Server) { d.srv = srv }

func (d *desktopTunnel) status() desktopTunnelStatus {
	d.mu.Lock()
	defer d.mu.Unlock()
	return d.statusLocked()
}

func (d *desktopTunnel) statusLocked() desktopTunnelStatus {
	state := d.state
	if state == "" {
		state = "off"
	}
	return desktopTunnelStatus{State: state, Error: d.error}
}

func (d *desktopTunnel) start() (desktopTunnelStatus, error) {
	d.mu.Lock()
	defer d.mu.Unlock()
	if d.state != "" && d.state != "off" {
		return d.statusLocked(), nil
	}
	if d.srv == nil {
		return d.statusLocked(), context.Canceled
	}
	tun, pairing, err := tunnel.NewHost(hubAddr, tunnel.DefaultRelayURL, d.srv.AccessKey(), d.onState)
	if err != nil {
		d.error = "start_failed"
		return d.statusLocked(), err
	}
	ctx, cancel := context.WithCancel(context.Background())
	d.cancel = cancel
	d.done = make(chan struct{})
	done := d.done
	d.state = "connecting"
	d.error = ""
	d.pairing = server.TunnelPairing{PairURL: pairing.URL, Relay: pairing.Relay, TunnelID: pairing.TunnelID}
	go func() {
		defer close(done)
		_ = tun.Serve(ctx)
	}()
	return d.statusLocked(), nil
}

func (d *desktopTunnel) onState(connected bool, err error) {
	d.mu.Lock()
	defer d.mu.Unlock()
	if d.cancel == nil || d.state == "stopping" {
		return
	}
	if connected {
		d.state = "connected"
		d.error = ""
		p := d.pairing
		d.srv.SetTunnelPairing(&p)
		return
	}
	// ponytail: one retry state covers initial dial failure and later drops;
	// the tunnel's existing backoff owns reconnection, not a second UI timer.
	d.state = "retrying"
	d.error = "relay_unavailable"
	d.srv.SetTunnelPairing(nil)
	slog.Warn("mobile tunnel disconnected; retrying", "err", err)
}

func (d *desktopTunnel) stop() desktopTunnelStatus {
	d.mu.Lock()
	if d.state == "stopping" {
		stopped := d.stopDone
		d.mu.Unlock()
		<-stopped
		return d.status()
	}
	if d.cancel == nil {
		d.state = "off"
		d.error = ""
		status := d.statusLocked()
		d.mu.Unlock()
		return status
	}
	cancel, done := d.cancel, d.done
	d.cancel = nil
	d.state = "stopping"
	d.stopDone = make(chan struct{})
	d.srv.SetTunnelPairing(nil)
	d.mu.Unlock()
	cancel()
	<-done
	d.mu.Lock()
	d.state = "off"
	d.error = ""
	status := d.statusLocked()
	close(d.stopDone)
	d.mu.Unlock()
	return status
}

func (d *desktopTunnel) mount(api func(string, http.HandlerFunc)) {
	api("GET /api/product/tunnel", func(w http.ResponseWriter, _ *http.Request) {
		writeDesktopTunnelStatus(w, http.StatusOK, d.status())
	})
	api("POST /api/product/tunnel/start", func(w http.ResponseWriter, _ *http.Request) {
		status, err := d.start()
		if err != nil {
			slog.Error("mobile tunnel start failed", "err", err)
			writeDesktopTunnelStatus(w, http.StatusInternalServerError, status)
			return
		}
		writeDesktopTunnelStatus(w, http.StatusOK, status)
	})
	api("POST /api/product/tunnel/stop", func(w http.ResponseWriter, _ *http.Request) {
		writeDesktopTunnelStatus(w, http.StatusOK, d.stop())
	})
}

func writeDesktopTunnelStatus(w http.ResponseWriter, code int, status desktopTunnelStatus) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(code)
	_ = json.NewEncoder(w).Encode(status)
}

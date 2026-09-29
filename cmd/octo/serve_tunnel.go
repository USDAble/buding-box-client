package main

import (
	"context"
	"fmt"
	"io"

	"github.com/open-octo/octo-agent/internal/server"
	"github.com/open-octo/octo-agent/internal/tunnel"
)

const defaultRelayURL = tunnel.DefaultRelayURL

// OCTO-FORK: CLI and desktop share one host setup so their pairing URLs and
// identity path cannot drift; the CLI keeps its existing headless output.
func startTunnel(ctx context.Context, srv *server.Server, addr, relayURL string, stdout io.Writer) error {
	tun, pairing, err := tunnel.NewHost(addr, relayURL, srv.AccessKey(), nil)
	if err != nil {
		return err
	}
	srv.SetTunnelPairing(&server.TunnelPairing{
		PairURL:  pairing.URL,
		Relay:    pairing.Relay,
		TunnelID: pairing.TunnelID,
	})
	printPairingMaterial(stdout, pairing)
	go func() { _ = tun.Serve(ctx) }()
	return nil
}

func printPairingMaterial(w io.Writer, pairing tunnel.Pairing) {
	fmt.Fprintln(w, "octo serve: managed tunnel enabled — pair a device with:")
	fmt.Fprintf(w, "  relay:      %s\n", pairing.Relay)
	fmt.Fprintf(w, "  tunnel id:  %s\n", pairing.TunnelID)
	fmt.Fprintf(w, "  host key:   %s\n", pairing.HostKey)
	fmt.Fprintf(w, "  pair token: %s  (one-time)\n", pairing.Token)
	fmt.Fprintf(w, "  pair url:   %s\n", pairing.URL)
	fmt.Fprintln(w, "  (or open Settings › Mobile in the web UI to scan a QR)")
}

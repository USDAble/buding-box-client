//go:build product_test && !product_production

package main

import (
	"fmt"
	"log"
	"net"
	"net/http"

	"github.com/open-octo/octo-agent/internal/productclient/clienttest"
	"github.com/open-octo/octo-agent/internal/productprofile"
)

// startProfileControlPlane starts the contract fixture only for the explicitly
// local test profile. Remote test profiles use their compiled HTTPS hosts and
// leave this process with no stand-in at all.
func startProfileControlPlane() error {
	if !productprofile.Current().UsesLocalStandin() {
		return nil
	}

	ln, err := net.Listen("tcp", "127.0.0.1:8788")
	if err != nil {
		return fmt.Errorf("start bundled testing control plane on 127.0.0.1:8788: %w", err)
	}
	go func() {
		if err := http.Serve(ln, clienttest.New().Handler()); err != nil && err != http.ErrServerClosed {
			log.Printf("testing control plane stopped: %v", err)
		}
	}()
	return nil
}

package main

import (
	"context"
	"net"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/open-octo/octo-agent/internal/server"
)

// TestPortableServer_WritesOnlyToDataRoot is the automated version of the
// acceptance item "using the product leaves no new .octo data in the host
// home" (需求 §8 / P1-便携数据根.md §6): a full server boot — the write-
// heaviest path in the desktop bootstrap — with the data root and the host
// home pointed at separate temp dirs must not create a .octo directory under
// the home, and everything it writes must land under the data root.
//
// It runs on a free loopback port (not the fixed hub port 127.0.0.1:8088,
// which may be occupied) and disables IM channels so the boot does not dial
// out or spawn bridge processes. NoMemory is left at its default so the
// memory tier exercises its real path — os.UserHomeDir() as a slug identity,
// with the data still landing under data/memories (internal/memory).
func TestPortableServer_WritesOnlyToDataRoot(t *testing.T) {
	dataRoot := t.TempDir()
	home := t.TempDir()
	t.Setenv("OCTO_DATA_ROOT", dataRoot)
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	t.Setenv("OCTO_HOOK_PRE_TURN", "")
	t.Setenv("OCTO_HOOK_POST_TURN", "")

	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}

	srv, err := server.New(server.Config{
		Tools:     true,
		NoChannel: true,
	})
	if err != nil {
		t.Fatal(err)
	}

	done := make(chan struct{})
	go func() {
		_ = srv.ServeOn(ln)
		close(done)
	}()

	// Wait until the listener is accepting connections, which is also the point
	// at which serveOn has finished its startup writes (initScheduler,
	// reconcileRegistry, store watch).
	deadline := time.Now().Add(20 * time.Second)
	for time.Now().Before(deadline) {
		if conn, err := net.DialTimeout("tcp", ln.Addr().String(), 200*time.Millisecond); err == nil {
			conn.Close()
			break
		}
		time.Sleep(50 * time.Millisecond)
	}

	// The host home must not gain a .octo data directory — the acceptance item
	// this test pins down.
	if _, err := os.Stat(filepath.Join(home, ".octo")); !os.IsNotExist(err) {
		t.Errorf("server boot created a host-home .octo directory (portable data belongs under the data root)")
	}

	// The data root must have been used, proving the boot actually ran its
	// write steps rather than silently no-op'ing.
	entries, err := os.ReadDir(dataRoot)
	if err != nil {
		t.Fatalf("read data root: %v", err)
	}
	if len(entries) == 0 {
		t.Errorf("data root %q is empty; expected the boot to materialize state there", dataRoot)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	_ = srv.Shutdown(ctx)
	select {
	case <-done:
	case <-time.After(10 * time.Second):
		t.Error("server did not stop after Shutdown")
	}
}

package main

import (
	"net"
	"os"
	"testing"
)

func TestCheckPort_Occupied(t *testing.T) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer ln.Close()
	f := checkPort(ln.Addr().String())
	if f == nil || f.Reason != "port" {
		t.Fatalf("checkPort(occupied) = %+v, want Reason \"port\"", f)
	}
}

func TestCheckPort_Free(t *testing.T) {
	// Bind a port, note its address, release it, then the probe must succeed —
	// the probe itself binds and immediately closes, so a free port stays free.
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	addr := ln.Addr().String()
	_ = ln.Close()
	if f := checkPort(addr); f != nil {
		t.Fatalf("checkPort(free) = %+v, want nil", f)
	}
}

func TestCheckSingleInstance_SameDirectoryLiveOwner(t *testing.T) {
	root := t.TempDir()
	t.Setenv("OCTO_DATA_ROOT", root)
	// Our own pid is alive, so this reads as "this exact directory is already
	// running" — the second launch should raise the window, not re-boot.
	if err := writeInstanceJSON(t.Name(), os.Getpid()); err != nil {
		t.Fatal(err)
	}
	activate, f := checkSingleInstance()
	if f != nil {
		t.Fatalf("checkSingleInstance = %+v, want no failure", f)
	}
	if !activate {
		t.Errorf("activateExisting = false, want true (own pid recorded and alive)")
	}
}

func TestCheckSingleInstance_NoOwnerOrDifferentDirectory(t *testing.T) {
	root := t.TempDir()
	t.Setenv("OCTO_DATA_ROOT", root)

	// No instance.json → not a same-directory relaunch (a different copy, or
	// a first launch).
	if activate, f := checkSingleInstance(); f != nil || activate {
		t.Fatalf("no instance.json: (activate=%v, f=%v), want (false, nil)", activate, f)
	}

	// A stale instance.json (dead pid) → also not a live same-directory
	// relaunch. 999999 is far above any real pid.
	if err := writeInstanceJSON(t.Name(), 999999); err != nil {
		t.Fatal(err)
	}
	if activate, f := checkSingleInstance(); f != nil || activate {
		t.Fatalf("stale instance.json: (activate=%v, f=%v), want (false, nil)", activate, f)
	}
}

func TestCanInplaceUpdate_DisabledForPortable(t *testing.T) {
	if canInplaceUpdate() {
		t.Errorf("canInplaceUpdate() = true, want false (portable product disables in-place update)")
	}
}

func TestAutostartAvailable_DisabledForPortable(t *testing.T) {
	b := &nativeBridge{}
	if b.AutostartAvailable() {
		t.Errorf("AutostartAvailable() = true, want false (portable product hides autostart)")
	}
}

package tunnel

import (
	"context"
	"net/url"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestNewHostSharesPairingAndPersistsIdentity(t *testing.T) {
	root := t.TempDir()
	t.Setenv("OCTO_DATA_ROOT", root)
	_, first, err := NewHost("127.0.0.1:8088", "wss://relay.example.com", "key", nil)
	if err != nil {
		t.Fatal(err)
	}
	_, second, err := NewHost("127.0.0.1:8088", "wss://relay.example.com", "key", nil)
	if err != nil {
		t.Fatal(err)
	}
	if first.TunnelID == "" || first.TunnelID != second.TunnelID || first.HostKey != second.HostKey {
		t.Fatalf("identity changed across starts: first=%+v second=%+v", first, second)
	}
	if first.Token == second.Token {
		t.Fatal("pairing token reused across starts")
	}
	u, err := url.Parse(first.URL)
	if err != nil {
		t.Fatal(err)
	}
	if u.Scheme != "octo-pair" || u.Host != "v1" || u.Query().Get("relay") != first.Relay || u.Query().Get("tid") != first.TunnelID || u.Query().Get("hk") != first.HostKey || u.Query().Get("tok") != first.Token {
		t.Fatalf("invalid pairing URL: %q", first.URL)
	}
	if _, err := os.Stat(filepath.Join(root, "tunnel.json")); err != nil {
		t.Fatalf("identity was not stored in the data root: %v", err)
	}
}

func TestNewHostRejectsInvalidRelayBeforeWritingIdentity(t *testing.T) {
	root := t.TempDir()
	t.Setenv("OCTO_DATA_ROOT", root)
	if _, _, err := NewHost("127.0.0.1:8088", "https://relay.example.com", "key", nil); err == nil {
		t.Fatal("non-WebSocket relay was accepted")
	}
	if _, err := os.Stat(filepath.Join(root, "tunnel.json")); !os.IsNotExist(err) {
		t.Fatalf("invalid relay wrote identity: %v", err)
	}
}

func TestTunnelReportsRelayConnectionAndDrop(t *testing.T) {
	relay := startStubRelay(t)
	states := make(chan bool, 2)
	tun, err := New(Config{
		RelayURL: wsScheme(relay.srv), TunnelID: "test-tunnel",
		LoopbackURL: "ws://127.0.0.1:8088/ws", AccessKey: "test-key",
		Logf:          func(string, ...any) {},
		OnStateChange: func(connected bool, _ error) { states <- connected },
	})
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go func() { _ = tun.Serve(ctx) }()
	relay.waitHost(t)
	for _, want := range []bool{true, false} {
		if !want {
			relay.mu.Lock()
			_ = relay.host.Close()
			relay.mu.Unlock()
		}
		select {
		case got := <-states:
			if got != want {
				t.Fatalf("connection state = %v, want %v", got, want)
			}
		case <-time.After(3 * time.Second):
			t.Fatalf("connection state %v was not reported", want)
		}
	}
}

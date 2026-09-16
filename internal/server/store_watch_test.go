package server

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"
)

// watchConn registers a connection on the hub and returns it, so a test can
// read what the watch broadcast. The hub's own run goroutine consumes the
// events channel and fans out to connections, so reading that channel directly
// races it — a registered connection is the observable side.
func watchConn(t *testing.T, srv *Server) *wsConn {
	t.Helper()
	conn := &wsConn{hub: srv.wsHub, send: make(chan []byte, 256), subscribed: map[string]struct{}{}}
	srv.wsHub.register <- conn
	return conn
}

// eventTypes drains the connection's queue and returns the "type" of each frame
// it received, waiting briefly for the hub's fan-out to land.
func eventTypes(t *testing.T, conn *wsConn) []string {
	t.Helper()
	var types []string
	deadline := time.After(2 * time.Second)
	for {
		select {
		case raw := <-conn.send:
			var probe struct{ Type string }
			if err := json.Unmarshal(raw, &probe); err != nil {
				t.Fatalf("unmarshal frame: %v", err)
			}
			types = append(types, probe.Type)
		case <-time.After(100 * time.Millisecond):
			return types
		case <-deadline:
			return types
		}
	}
}

func contains(types []string, want string) bool {
	for _, t := range types {
		if t == want {
			return true
		}
	}
	return false
}

// TestStoreWatch_AnnouncesASessionFromAnotherProcess: a session created outside
// this process (what every `octo` run in a terminal does) emits no broadcast of
// its own, so an open sidebar sits on a stale list until something else happens
// to refresh it. The watch is what notices.
func TestStoreWatch_AnnouncesASessionFromAnotherProcess(t *testing.T) {
	srv := groupTestServer(t)
	conn := watchConn(t, srv)

	prev := sampleStore()
	eventTypes(t, conn) // drain anything setup emitted

	// Stands in for the terminal: a transcript appears on disk with nothing
	// telling this server about it.
	saveSessionWithDir(t, t.TempDir())

	cur := srv.pollStoreOnce(prev)
	if got := eventTypes(t, conn); !contains(got, "session_created") {
		t.Errorf("no session_created after a transcript appeared; got %v", got)
	}
	if cur.sessionCount != prev.sessionCount+1 {
		t.Errorf("sessionCount = %d, want %d", cur.sessionCount, prev.sessionCount+1)
	}
}

// TestStoreWatch_AnnouncesAProjectFromAnotherProcess: same for the registry.
// EnsureProjectForDir is the CLI's path and cannot reach this process's
// broadcast hook (notifyGroupsChanged is installed per-process).
func TestStoreWatch_AnnouncesAProjectFromAnotherProcess(t *testing.T) {
	srv := groupTestServer(t)
	conn := watchConn(t, srv)

	prev := sampleStore()
	eventTypes(t, conn)

	if err := EnsureProjectForDir(t.TempDir(), "sess-elsewhere"); err != nil {
		t.Fatalf("EnsureProjectForDir: %v", err)
	}

	srv.pollStoreOnce(prev)
	if got := eventTypes(t, conn); !contains(got, "session_groups_changed") {
		t.Errorf("no session_groups_changed after the registry changed; got %v", got)
	}
}

// TestStoreWatch_QuietWhenNothingChanged is what makes the watch affordable:
// the frontend answers each broadcast by refetching the whole session list,
// which loads every transcript, so announcing an unchanged store every tick
// would be a standing cost for every open tab.
func TestStoreWatch_QuietWhenNothingChanged(t *testing.T) {
	srv := groupTestServer(t)
	conn := watchConn(t, srv)
	saveSessionWithDir(t, t.TempDir())

	prev := sampleStore()
	eventTypes(t, conn)

	for i := 0; i < 3; i++ {
		prev = srv.pollStoreOnce(prev)
	}
	if got := eventTypes(t, conn); len(got) != 0 {
		t.Errorf("idle polls broadcast %v, want nothing", got)
	}
}

// TestStoreWatch_TurnWritesDoNotAnnounce pins the property that makes the watch
// affordable at all: writing to an existing transcript must be invisible to it.
// Every broadcast makes every open tab re-list sessions, which loads every
// transcript, so a fingerprint that moved on each turn would be a standing cost
// for the length of every conversation.
//
// It holds because Session.Save appends, and its rewrite path truncates in
// place (O_TRUNC) rather than renaming a temp file over the old one — a rename
// WOULD bump the directory's mtime and put this watch in exactly that loop.
func TestStoreWatch_TurnWritesDoNotAnnounce(t *testing.T) {
	srv := groupTestServer(t)
	conn := watchConn(t, srv)
	sess := saveSessionWithDir(t, t.TempDir())

	prev := sampleStore()
	eventTypes(t, conn)

	// A rewrite of an existing transcript — the shape a turn produces.
	if err := sess.SetTitle("a new title"); err != nil {
		t.Fatalf("SetTitle: %v", err)
	}
	if err := sess.Save(); err != nil {
		t.Fatalf("save: %v", err)
	}

	srv.pollStoreOnce(prev)
	if got := eventTypes(t, conn); contains(got, "session_created") {
		t.Errorf("a write to an existing session announced a new one; got %v", got)
	}
}

// TestStoreWatch_StopsWhenTold: the watch must not outlive the server, or every
// server in a test binary — and every restart of the daemon — leaks a ticker
// goroutine. doShutdown closes watchStop; this covers the goroutine honouring
// it.
//
// The channel is closed through a local copy rather than through the field.
// The running goroutine reads s.watchStop, so touching the field here would be
// a write racing that read — which is exactly what -race caught when this test
// nil'd the field out afterwards.
func TestStoreWatch_StopsWhenTold(t *testing.T) {
	srv := groupTestServer(t)
	stop := srv.watchStop
	if stop == nil {
		t.Fatal("watchStop was not created")
	}
	srv.startStoreWatch()

	close(stop)
	time.Sleep(20 * time.Millisecond)
}

// TestStoreWatch_SampleCreatesNothing is V-105's first nail, and the flake that
// took three CI rounds was its symptom rather than its subject.
//
// OCTO-FORK: added with the read-only sample fix — see dev-docs-usdable/需求/20260911/需求基线.md §5.6.
//
// The watch is a READER of the data root. Its sample used to resolve through the
// creating accessors — agent.SessionsDir, and sessionGroupsPath via
// datapath.Root — and datapath.Root does not merely join a path: it makes the
// directory and then writes a temporary probe file into it to prove the volume
// accepts writes. A sampler that aims that at an empty root therefore creates
// data/sessions in it, every five seconds, for the life of the server.
//
// Two consequences, and the product one is the reason this is not just test
// hygiene. In a test the write landed during a t.TempDir() removal, which fails
// with "directory not empty" against whichever test happened to be running. In
// the product it is worse in kind: a sampler can recreate a data root the user
// has just deleted, which is the harm datapath.Freeze exists to prevent — an
// empty data/ looks exactly like a clean install while the real one is on a
// drive that is no longer mounted.
func TestStoreWatch_SampleCreatesNothing(t *testing.T) {
	root := filepath.Join(t.TempDir(), "data") // deliberately absent
	t.Setenv("OCTO_DATA_ROOT", root)

	_ = sampleStore()

	if _, err := os.Stat(root); !os.IsNotExist(err) {
		entries, _ := os.ReadDir(root)
		names := make([]string, 0, len(entries))
		for _, e := range entries {
			names = append(names, e.Name())
		}
		t.Errorf("a sample materialised %s (entries: %v); the watch must read the root, not create it", root, names)
	}
}

// TestStoreWatch_ShutdownWaitsForASampleInFlight is the other half of V-105.
//
// Closing watchStop tells the watch to stop; it does not wait for it. A sample
// already running when the close lands keeps resolving the data root — and it
// resolves it late, so it writes into whatever root is current by the time it
// gets there. That is the cross-test write the harness saw: the test that owned
// the root had already finished, its cleanup had already returned, and the
// removal was reported against a different test's name each run.
//
// So the property is an ordering, and an ordering needs a barrier to observe at
// all: the test holds a sample open (storeSampleBarrier), then requires that
// Shutdown has not returned while it is still held, and that it does return once
// it is released. Both directions fail on a revert — the first select fires if
// Shutdown stops waiting, the second hits its timeout if the wait deadlocks.
func TestStoreWatch_ShutdownWaitsForASampleInFlight(t *testing.T) {
	srv := groupTestServer(t)

	inSample := make(chan struct{})
	release := make(chan struct{})
	var once sync.Once
	srv.storeSampleBarrier = func() {
		once.Do(func() { close(inSample) })
		<-release
	}
	srv.startStoreWatch()
	select {
	case <-inSample: // a sample is now running and will not finish on its own
	case <-time.After(2 * time.Second):
		t.Fatal("the watch never sampled; the barrier was not reached, so this test would prove nothing")
	}

	shutdownDone := make(chan struct{})
	go func() {
		defer close(shutdownDone)
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_ = srv.Shutdown(ctx)
	}()

	select {
	case <-shutdownDone:
		t.Error("Shutdown returned while a store sample was still in flight; that sample writes into the data root after the caller has released it (V-105)")
	case <-time.After(200 * time.Millisecond):
	}

	close(release)
	select {
	case <-shutdownDone:
	case <-time.After(5 * time.Second):
		t.Error("Shutdown never returned after the in-flight sample finished")
	}
}

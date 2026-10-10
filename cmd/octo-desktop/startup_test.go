package main

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"net/url"
	"runtime"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func TestStartupTargetHasBackendWindowIdentityBeforeAssembly(t *testing.T) {
	oldToken, oldErr := windowTokenVal, windowTokenErr
	windowTokenOnce = sync.Once{}
	windowTokenVal, windowTokenErr = "", nil
	t.Cleanup(func() {
		windowTokenOnce = sync.Once{}
		windowTokenVal, windowTokenErr = oldToken, oldErr
		if oldToken != "" || oldErr != nil {
			windowTokenOnce.Do(func() {})
		}
	})
	b := &nativeBridge{url: "http://127.0.0.1:8088", backendReady: make(chan struct{})}
	// No app is needed to retain a route before the backend and WebView exist.
	b.showWindowAt("settings")
	if b.startupTarget.Load() == nil {
		t.Fatal("startup route was not retained")
	}
	target, err := url.Parse(*b.startupTarget.Load())
	if err != nil {
		t.Fatal(err)
	}
	token := target.Query().Get(windowTokenQuery)
	if token == "" || token != windowToken() {
		t.Fatal("startup URL does not carry the backend's window identity")
	}
	if target.Fragment != "settings" {
		t.Fatal("startup identity initialization lost the requested route")
	}
}

// OCTO-FORK: pending routes must survive both cold startup and a warm window recreation.
func TestStartupTargetRetainsLatestTrayRoute(t *testing.T) {
	for _, ready := range []bool{false, true} {
		b := &nativeBridge{url: "http://127.0.0.1:8088", backendReady: make(chan struct{})}
		if ready {
			close(b.backendReady)
		}
		initialURL := "/startup"
		if ready {
			initialURL = shellURL(b.url, "")
		}
		b.showWindowAt("settings")
		b.showWindowAt("new")
		b.showWindow()
		target, err := url.Parse(b.takeStartupTarget(initialURL))
		if err != nil || target.Fragment != "new" || target.Query().Get(windowTokenQuery) != windowToken() {
			t.Fatalf("ready=%v: latest tray route or window identity lost: %v", ready, target)
		}
		if b.takeStartupTarget(initialURL) != "" {
			t.Fatal("consumed route would be replayed on a later window")
		}
	}
	initialURL := shellURL("http://127.0.0.1:8088", "settings")
	b := &nativeBridge{}
	b.startupTarget.Store(&initialURL)
	if b.takeStartupTarget(initialURL) != "" || b.startupTarget.Load() != nil {
		t.Fatal("an unchanged target causes a redundant navigation")
	}
}

func TestProcessStartTimingIncludesDependencyInitialization(t *testing.T) {
	if runtime.GOOS != "darwin" && runtime.GOOS != "windows" {
		t.Skip("native process timing is available on desktop target platforms")
	}
	startedAt, err := platformProcessStartedAt()
	if err != nil {
		t.Fatal(err)
	}
	if startedAt.IsZero() || startedAt.After(desktopStartedAt) {
		t.Fatalf("process creation %v must precede desktop package initialization %v", startedAt, desktopStartedAt)
	}
}

func TestNativeStartupCleanupIsOnce(t *testing.T) {
	var closed atomic.Int32
	cleanup := func() { closed.Add(1) }
	b := &nativeBridge{}
	b.startupClose.Store(&cleanup)
	var calls sync.WaitGroup
	for range 8 {
		calls.Go(b.stopNativeStartup)
	}
	calls.Wait()
	if closed.Load() != 1 || b.startupClose.Load() != nil {
		t.Fatalf("startup window cleaned up %d times", closed.Load())
	}
}

func TestDesktopStartupPageIndependentOfHub(t *testing.T) {
	prior := active.Load()
	t.Cleanup(func() { active.Store(prior) })
	for _, copy := range []*uiStrings{&enStrings, &zhStrings} {
		active.Store(copy)
		w := httptest.NewRecorder()
		serveDesktopStartup(w, httptest.NewRequest(http.MethodGet, "/startup", nil))
		body := w.Body.String()
		if w.Code != http.StatusOK || !strings.Contains(body, copy.trayStarting) {
			t.Fatalf("startup page: status=%d body=%s", w.Code, body)
		}
		if strings.Contains(body, "<script") || strings.Contains(body, "src=") || strings.Contains(body, "href=") {
			t.Fatal("loading page depends on another resource")
		}
		if !strings.Contains(body, `aria-busy="true"`) || !strings.Contains(body, "animation: spin") {
			t.Fatal("loading status or spinner missing")
		}
	}
	w := httptest.NewRecorder()
	serveDesktopStartup(w, httptest.NewRequest(http.MethodGet, "/api/health", nil))
	if w.Code != http.StatusNotFound {
		t.Fatal("startup asset handler impersonates the backend")
	}
}

func TestDesktopStartupReadiness(t *testing.T) {
	b := &nativeBridge{backendReady: make(chan struct{})}
	if b.backendIsReady() {
		t.Fatal("hub considered ready before health check")
	}
	close(b.backendReady)
	if !b.backendIsReady() {
		t.Fatal("hub still starting after health check")
	}
}

// OCTO-FORK: a listening socket and a generic 200 page are not service readiness.
func TestWaitBackendReady(t *testing.T) {
	var calls atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/health" {
			t.Errorf("unexpected probe path: %s", r.URL.Path)
		}
		if calls.Add(1) == 1 {
			w.Write([]byte("<html>not ready</html>"))
			return
		}
		w.Write([]byte(`{"status":"ok"}`))
	}))
	defer srv.Close()
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	if err := waitBackendReady(ctx, srv.URL); err != nil {
		t.Fatal(err)
	}
	if calls.Load() < 2 {
		t.Fatal("accepted a generic 200 page as readiness")
	}
}

func TestWaitBackendReadyTimeout(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusServiceUnavailable)
		w.Write([]byte(`{"status":"ok"}`))
	}))
	defer srv.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()
	if err := waitBackendReady(ctx, srv.URL); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("got %v, want readiness timeout", err)
	}
}

func TestWaitBackendReadyCancelled(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if err := waitBackendReady(ctx, "http://127.0.0.1:1"); !errors.Is(err, context.Canceled) {
		t.Fatalf("got %v, want cancellation", err)
	}
}

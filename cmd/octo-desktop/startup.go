package main

import (
	"context"
	_ "embed"
	"encoding/json"
	"fmt"
	"html/template"
	"io"
	"log/slog"
	"net/http"
	"runtime"
	"sync"
	"time"

	"github.com/open-octo/octo-agent/internal/brand"
	"github.com/wailsapp/wails/v3/pkg/application"
	"github.com/wailsapp/wails/v3/pkg/events"
)

// OCTO-FORK: navigation must wait for HTTP readiness, not merely a bound port.
var desktopStartedAt = time.Now()

//go:embed startup.html
var desktopStartupHTML string

var desktopStartupTemplate = template.Must(template.New("startup").Parse(desktopStartupHTML))

func (b *nativeBridge) startNativeStartup() (func(string), error) {
	// Wails locks main to the UI thread in init; its dispatcher is not ready yet.
	closeWindow, setMessage, err := platformStartupWindow(brand.Load().Name(brand.DefaultLocale), L().trayStarting, func() { b.app.Quit() })
	if err != nil {
		return nil, err
	}
	if closeWindow != nil {
		b.startupClose.Store(&closeWindow)
	}
	return setMessage, nil
}

func (b *nativeBridge) stopNativeStartup() {
	if closeWindow := b.startupClose.Swap(nil); closeWindow != nil {
		(*closeWindow)()
	}
}

// OCTO-FORK: Wails serves this page without waiting for hub assembly or app assets.
func serveDesktopStartup(w http.ResponseWriter, r *http.Request) {
	if r.URL.Path != "/startup" || (r.Method != http.MethodGet && r.Method != http.MethodHead) {
		http.NotFound(w, r)
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	if r.Method == http.MethodHead {
		return
	}
	if err := desktopStartupTemplate.Execute(w, struct{ Starting string }{L().trayStarting}); err != nil {
		slog.Error("desktop startup page", "error", err)
	}
}

func (b *nativeBridge) backendIsReady() bool {
	if b.backendReady == nil {
		return true
	}
	select {
	case <-b.backendReady:
		return true
	default:
		return false
	}
}

// OCTO-FORK: consume the latest tray route once, including backend-ready recreations.
func (b *nativeBridge) takeStartupTarget(initialURL string) string {
	if target := b.startupTarget.Swap(nil); target != nil && *target != initialURL {
		return *target
	}
	return ""
}

func (b *nativeBridge) prepareStartupWindow(w *application.WebviewWindow, starting bool, initialURL string) {
	// External hub pages have no Wails runtime; use native navigation events.
	navigation := events.Mac.WebViewDidFinishNavigation
	switch runtime.GOOS {
	case "windows":
		navigation = events.Windows.WebViewNavigationCompleted
	case "linux":
		navigation = events.Linux.WindowLoadFinished
	}
	loaded := make(chan struct{})
	var once sync.Once
	w.OnWindowEvent(navigation, func(*application.WindowEvent) {
		once.Do(func() {
			if b.currentWindow() != w {
				return
			}
			// OCTO-FORK: a tray action during warm first-navigation must not disappear.
			if !starting {
				if target := b.takeStartupTarget(initialURL); target != "" {
					w.SetURL(target)
				}
			}
			b.windowReady.Store(true)
			if !b.hidden.Load() {
				w.Show()
				w.Focus()
				b.stopNativeStartup()
				logDesktopStartup("loading_visible")
			}
			close(loaded)
		})
	})
	if !starting {
		return
	}
	go func() {
		for _, ready := range []<-chan struct{}{loaded, b.backendReady} {
			select {
			case <-ready:
			case <-b.app.Context().Done():
				return
			}
		}
		if b.currentWindow() == w {
			if target := b.takeStartupTarget(initialURL); target != "" {
				w.SetURL(target)
				logDesktopStartup("app_navigation_started")
			}
		}
	}()
}

func waitBackendReady(ctx context.Context, baseURL string) error {
	// The probe is local; host proxy settings must not redirect it.
	transport := &http.Transport{Proxy: nil}
	defer transport.CloseIdleConnections()
	client := &http.Client{Transport: transport, Timeout: 500 * time.Millisecond}
	ticker := time.NewTicker(100 * time.Millisecond)
	defer ticker.Stop()
	for {
		if err := ctx.Err(); err != nil {
			return fmt.Errorf("local service did not become ready: %w", err)
		}
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, baseURL+"/api/health", nil)
		if err != nil {
			return err
		}
		res, err := client.Do(req)
		if err == nil {
			var health struct {
				Status string `json:"status"`
			}
			decodeErr := json.NewDecoder(io.LimitReader(res.Body, 1024)).Decode(&health)
			res.Body.Close()
			if res.StatusCode == http.StatusOK && decodeErr == nil && health.Status == "ok" {
				return nil
			}
		}
		select {
		case <-ctx.Done():
			return fmt.Errorf("local service did not become ready: %w", ctx.Err())
		case <-ticker.C:
		}
	}
}

func logDesktopStartup(stage string) {
	logDesktopStartupAt(stage, time.Now())
}

func logDesktopStartupAt(stage string, at time.Time) {
	slog.Info("desktop startup", "stage", stage, "elapsed_ms", at.Sub(desktopStartedAt).Milliseconds())
}

func logDesktopProcessTiming(mainEnteredAt, nativeVisibleAt time.Time, visible bool) {
	startedAt, err := platformProcessStartedAt()
	if err != nil {
		slog.Warn("desktop process timing unavailable", "error", err)
		return
	}
	if startedAt.IsZero() || startedAt.After(mainEnteredAt) {
		return
	}
	attrs := []any{"process_to_main_ms", mainEnteredAt.Sub(startedAt).Milliseconds()}
	if visible {
		attrs = append(attrs, "process_to_native_ms", nativeVisibleAt.Sub(startedAt).Milliseconds())
	}
	slog.Info("desktop process timing", attrs...)
}

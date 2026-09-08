// Boot-time failure chain for the desktop shell. Each stage below is an
// explicit, failable step that produces a user-visible FatalDialog instead of
// a panic to a black screen. See dev-docs-usdable/需求/2260906/技术方案/P2-启动与生命周期.md §3.1.
package main

import (
	"encoding/json"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"time"

	"github.com/open-octo/octo-agent/internal/datapath"
	"github.com/open-octo/octo-agent/internal/serveproc"
	"github.com/wailsapp/wails/v3/pkg/application"
)

// BootFailure describes a boot stage that cannot proceed. Reason is a stable
// machine key ("webview2" | "dataroot" | "port") that fatalDialog maps to
// user-facing text; Detail carries the path, error, or process name for the
// more specific message.
type BootFailure struct {
	Reason string
	Detail string
}

// checkWebView2 fails when the WebView2 Evergreen Runtime is missing. The
// window cannot render without it, so this is a fatal, not a degradation.
func checkWebView2() *BootFailure {
	if webview2Available() {
		return nil
	}
	return &BootFailure{Reason: "webview2"}
}

// checkDataRoot fails when the data root cannot be created or is not writable.
// datapath.Root has no fallback (P1): an unusable data/ is a hard error, and
// this is where that error becomes a readable dialog instead of a silent
// "host stays clean but product is broken" failure.
func checkDataRoot() *BootFailure {
	if _, err := datapath.Root(); err != nil {
		return &BootFailure{Reason: "dataroot", Detail: err.Error()}
	}
	return nil
}

// checkSingleInstance reports whether this exact portable directory is already
// running. The Wails SingleInstance mutex is keyed on the product identifier —
// shared by every portable copy on the machine — so it cannot tell two copies
// apart. instance.json is per-directory (it lives in each copy's own data/),
// so a live pid recorded there means "this same directory is already up", and
// the second launch should just raise its window without any prompt.
func checkSingleInstance() (activateExisting bool, f *BootFailure) {
	inst, ok := readInstanceJSON()
	if !ok {
		return false, nil
	}
	return serveproc.IsAlive(inst.PID), nil
}

// checkPort fails when the fixed hub port is already bound by another program
// (including an upstream Octo or a stray `octo serve`). The probe binds and
// immediately releases; a tiny TOCTOU window remains, but a real collision
// still lands in startHub's own listen failure and is reported identically.
func checkPort(addr string) *BootFailure {
	ln, err := net.Listen("tcp", addr)
	if err != nil {
		return &BootFailure{Reason: "port", Detail: addr}
	}
	_ = ln.Close()
	return nil
}

// instanceJSON records which process owns this data root. It is the
// per-directory half of single-instance detection: the first instance writes
// it on startup and removes it on clean quit, and the watchdog uses the pid to
// confirm the data root that came back is still this process's (P2 §3.4).
type instanceJSON struct {
	Exe       string `json:"exe"`
	PID       int    `json:"pid"`
	StartedAt string `json:"startedAt"`
}

func instanceJSONPath() (string, error) {
	return datapath.Join("instance.json")
}

func writeInstanceJSON(exe string, pid int) error {
	path, err := instanceJSONPath()
	if err != nil {
		return err
	}
	b, err := json.Marshal(instanceJSON{Exe: exe, PID: pid, StartedAt: time.Now().Format(time.RFC3339)})
	if err != nil {
		return err
	}
	return os.WriteFile(path, b, 0o644)
}

func removeInstanceJSON() {
	if path, err := instanceJSONPath(); err == nil {
		_ = os.Remove(path)
	}
}

func readInstanceJSON() (instanceJSON, bool) {
	path, err := instanceJSONPath()
	if err != nil {
		return instanceJSON{}, false
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return instanceJSON{}, false
	}
	var inst instanceJSON
	if err := json.Unmarshal(data, &inst); err != nil {
		return instanceJSON{}, false
	}
	return inst, true
}

// fatalDialog shows a native error dialog for a boot failure and ends the
// process on its single button. It uses Wails' platform dialog rather than an
// HTML page because a missing WebView2 is precisely one of the reasons to be
// here, and a webview cannot render its own error. The "Quit" action ends the
// process directly (os.Exit) — requirement §5.1.2-4 wants a real exit, not a
// hide-to-tray and not a logout.
func fatalDialog(app *application.App, bridge *nativeBridge, f *BootFailure) {
	title, message := fatalMessage(f)
	dlg := app.Dialog.Error().SetTitle(title).SetMessage(message)
	dlg.AddButton(L().fatalQuit).OnClick(func() { os.Exit(1) })
	// A missing WebView2 gets a second action: 需求 §5.1.2-7 wants the user
	// pointed at the download/install page, not merely told a component is
	// absent. Opening the browser is best-effort — the quit button is the
	// guaranteed way out regardless.
	if f.Reason == "webview2" {
		dlg.AddButton(L().fatalWebView2Download).OnClick(func() {
			_ = bridge.OpenExternal(webview2DownloadURL)
		})
	}
	dlg.Show()
}

// webview2DownloadURL is Microsoft's WebView2 download/install page, opened by
// the FatalDialog's second button when the runtime is missing (需求 §5.1.2-7).
const webview2DownloadURL = "https://developer.microsoft.com/microsoft-edge/webview2/"

// fatalMessage maps a boot failure to the dialog title and body for the
// active language. The port message names the upstream product "Octo" as an
// external program — // upstream product name, not our brand — per requirement
// §5.1.2-6.
func fatalMessage(f *BootFailure) (title, message string) {
	title = L().errTitle
	switch f.Reason {
	case "webview2":
		return title, L().fatalWebView2
	case "dataroot":
		return title, fmt.Sprintf(L().fatalDataRootFmt, filepath.FromSlash(f.Detail))
	case "port":
		// The port message is fixed text ("Port 8088 …", 需求 §5.1.2-6); no
		// detail is interpolated, so the address stays out of the copy.
		return title, L().fatalPortFmt
	default:
		return title, fmt.Sprintf(L().fatalUnknownFmt, f.Reason)
	}
}

// selfExePath returns the absolute path of this executable, or "" on failure.
// It is the portable product's identity: single-instance (AdditionalData) and
// instance.json both key on the executable path, because every copy lives
// beside its own data/ on its own volume — the same path means the same copy.
func selfExePath() string {
	exe, err := os.Executable()
	if err != nil {
		return ""
	}
	if abs, err := filepath.Abs(exe); err == nil {
		return abs
	}
	return exe
}

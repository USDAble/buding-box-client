// Command octo-desktop is octo's native desktop shell — and, while it runs, the
// single backend every other octo interface shares. It binds the fixed loopback
// port octo serve uses (127.0.0.1:8088), runs the same web server in-process,
// and points a Wails window at it, so the Svelte frontend and every /api and
// /ws handler are reused unchanged and the Web UI / VS Code / Obsidian / CLI all
// connect to this one instance. On top of the server it adds the native layer a
// browser can't reach — OS folder dialog, tray, launch-at-login, notifications —
// wired in through server.NativeBridge.
//
// Only one backend owns the port at a time: the app joins the data-root serve.pid
// protocol (internal/serveproc) that `octo serve -d` uses, offering to take over
// a running daemon rather than fighting for the port.
//
// See dev-docs/desktop-hub-design.md and dev-docs/wails-desktop-design.md.
package main

import (
	"context"
	"crypto/rand"
	_ "embed"
	"encoding/hex"
	"fmt"
	"log"
	"log/slog"
	"net"
	"net/url"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync/atomic"
	"time"

	"github.com/mattn/go-isatty"
	"github.com/open-octo/octo-agent/internal/brand"
	"github.com/open-octo/octo-agent/internal/crashlog"
	"github.com/open-octo/octo-agent/internal/datapath"
	"github.com/open-octo/octo-agent/internal/logfile"
	"github.com/open-octo/octo-agent/internal/productprofile"
	"github.com/open-octo/octo-agent/internal/serveenv"
	"github.com/open-octo/octo-agent/internal/serveproc"
	"github.com/open-octo/octo-agent/internal/server"
	"github.com/open-octo/octo-agent/internal/shellpath"
	"github.com/open-octo/octo-agent/internal/upgrade"
	"github.com/open-octo/octo-agent/internal/version"
	"github.com/wailsapp/wails/v3/pkg/application"
	"github.com/wailsapp/wails/v3/pkg/events"
	"github.com/wailsapp/wails/v3/pkg/services/notifications"
	"github.com/wailsapp/wails/v3/pkg/updater"
)

// Tray icons. macOS wants a monochrome template image (auto-tinted for the
// light/dark menu bar); Windows/Linux want the regular color icon. A tray with
// neither an icon nor a label is invisible on macOS, which is why one must be
// set explicitly.
//
//go:embed build/darwin/tray-icon.png
var trayTemplateIcon []byte

//go:embed build/linux/icon.png
var trayColorIcon []byte

// hubAddr is the fixed loopback address the hub owns — the same default
// `octo serve` binds, so every existing client (Web, VS Code, Obsidian, CLI)
// finds it without configuration. LAN exposure stays a CLI concern.
const hubAddr = "127.0.0.1:8088"

// OCTO-FORK: newWindowToken mints the process-in-memory product-gate token —
// 32 random bytes, hex-encoded — which lets the product gate recognise "this
// desktop window's requests" (see internal/productgate). Memory-only: a
// restart mints a fresh one, so there is nothing to persist or revoke. See
// dev-docs-usdable/需求/2260906/技术方案/P3-登录态与产品门.md.
func newWindowToken() string {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		// crypto/rand failing is essentially unheard of; a timestamp token
		// keeps the window usable rather than refusing to open.
		return fmt.Sprintf("octo-%d", time.Now().UnixNano())
	}
	return hex.EncodeToString(b)
}

// desktopWebviewURL returns the URL the Wails window loads. It defaults to the
// in-process hub (hubAddr), whose server serves the embedded webdist.
// OCTO_DESKTOP_DEV_URL is accepted only by a developer Profile for the shell +
// Vite hot-reload loop; a production build ignores it. See
// dev-docs-usdable/运行时Profile配置.md.
func desktopWebviewURL() string {
	if productprofile.Current().AllowDevWebview {
		if dev := strings.TrimSpace(os.Getenv("OCTO_DESKTOP_DEV_URL")); dev != "" {
			return dev
		}
	}
	return "http://" + hubAddr
}

// applyDesktopProfile removes developer-only process inputs before any desktop
// startup code can read the data root, serve.env, or server configuration. It
// complements (rather than replaces) the immutable build-selected profile:
// clearing the inputs makes accidental release-environment leakage harmless.
func applyDesktopProfile() productprofile.Profile {
	p := productprofile.Current()
	if !p.AllowDevWebview {
		os.Unsetenv("OCTO_DESKTOP_DEV_URL")
	}
	if !p.AllowDataRootOverride {
		os.Unsetenv("OCTO_DATA_ROOT")
	}
	if !p.AllowEnvironmentModelSource {
		os.Unsetenv("OCTO_PROVIDER")
		for _, entry := range os.Environ() {
			name, _, ok := strings.Cut(entry, "=")
			if ok && (strings.HasSuffix(name, "_MODEL") || strings.HasSuffix(name, "_API_KEY")) {
				os.Unsetenv(name)
			}
		}
	}
	return p
}

// isBundled reports whether we're running inside a .app. The Wails
// notifications service needs a bundle identifier and hard-fails startup
// without one, so it's registered only when bundled — a bare `make desktop`
// binary still runs, just without native notifications.
func isBundled() bool {
	exe, err := os.Executable()
	if err != nil {
		return false
	}
	return strings.Contains(exe, ".app/Contents/MacOS/")
}

// notificationsAvailable reports whether the OS-native notification service can
// be created on this platform. macOS's UNUserNotificationCenter needs a bundle
// identifier, so it only works from a .app bundle; Windows (a registered COM
// toast activator) and Linux (D-Bus) work for any running executable, so the
// tray update toast reaches all three platforms uniformly.
func notificationsAvailable() bool {
	if runtime.GOOS == "darwin" {
		return isBundled()
	}
	return true
}

// homeIfRootLaunch returns the directory the hub should switch to given its
// inherited working dir and the user's home, or "" to keep the inherited dir.
// A GUI launcher drops a double-clicked app at a filesystem root — "/" for
// Finder/LaunchServices on macOS and several Linux .desktop launchers, a drive
// root like "C:\" on Windows — detected here as a path that is its own parent;
// an os.Getwd error surfaces as an empty wd. In those cases home is the sane
// default. A meaningful cwd (a terminal launch from a project dir) is left
// untouched. Not covered: a Windows launch that inherits a system directory
// such as C:\Windows\System32 — that is not a root, so this leaves it in place.
func homeIfRootLaunch(wd, home string) string {
	if home == "" {
		return ""
	}
	if wd == "" || filepath.Dir(wd) == wd {
		return home
	}
	return ""
}

// ensureWorkingDir pins the process cwd to the program directory, so a
// Finder/desktop launch from "/" (or another filesystem root) can't seed skill
// discovery and project-memory from the filesystem root. Unlike upstream —
// which chdir'd into the user's home — the portable product pins to the
// program directory (data root parent), matching the CLI.
// OCTO-FORK: cwd is pinned to the program dir, not the host home — see
// dev-docs-usdable/需求/2260906/技术方案/P1-便携数据根.md §3.3.
func ensureWorkingDir() {
	dir, err := datapath.ProgramDir()
	if err != nil {
		return
	}
	_ = os.Chdir(dir)
}

// ensureValidTempDir unsets $TMPDIR when it doesn't point at a usable
// directory (missing, or a file rather than a directory), so Go's
// os.TempDir() (and the Wails updater's os.MkdirTemp("", ...) on top of it)
// falls back to the platform default instead of failing outright.
func ensureValidTempDir() {
	dir := os.Getenv("TMPDIR")
	if dir == "" {
		return
	}
	if info, err := os.Stat(dir); err != nil || !info.IsDir() {
		os.Unsetenv("TMPDIR")
	}
}

func main() {
	profile := applyDesktopProfile()

	// macOS's postinstall script launches the app with `open` from inside
	// installd's ephemeral PKInstallSandbox.*; the launched process can inherit
	// that sandbox's $TMPDIR. The desktop app then runs for days as a tray
	// resident, long after the sandbox is torn down, so a later update check's
	// os.MkdirTemp fails with "no such file or directory" against a $TMPDIR
	// that hasn't existed since install. Clear it before anything (including
	// the updater helper below) can use it.
	ensureValidTempDir()

	// Point stderr at the data-root crash.log before anything that can die runs. This
	// process has no usable stderr of its own (Windows: built with -H windowsgui,
	// so no console; macOS: launched from Finder), and the runtime writes panic
	// traces straight to the descriptor — below the slog/log redirection
	// setupHubLog installs later. Without this, an unrecovered panic in any
	// goroutine closes the window and leaves nothing behind to report. Ahead of
	// the helper mode below on purpose: swapping a staged update over a running
	// install is exactly the kind of file work whose failures need a record.
	setupCrashLog()

	// When spawned as the updater's helper child (sentinel env vars set), swap
	// the staged update over the installed app and exit — before any of the
	// launch side effects below (chdir, CLI/uv seeding, settings) run in a
	// process that only exists to copy files. No-op on a normal launch;
	// application.New would also catch it, just later.
	updater.HandleHelperMode()

	// A GUI launch inherits "/" as the working directory; move to the user's
	// home before anything reads it (the in-process server records its launch
	// dir as the skill-discovery and project-memory root).
	ensureWorkingDir()

	// A GUI launch also inherits a minimal PATH (macOS: no ~/.local/bin,
	// /opt/homebrew/bin; Linux: a .desktop/systemd launch may skip the login
	// profile entirely). The server runs in-process here, so stdio MCP children
	// and shell tools inherit this process's PATH directly — sync it to the login
	// shell's before server.New below, mirroring the `octo serve` binary.
	shellpath.SyncToLoginShell()

	// Load the data-root serve.env for variables a GUI launch can't inherit from a
	// login shell (e.g. TAVILY_API_KEY, provider keys). Best-effort — missing
	// file is a no-op, explicit env always wins. Mirrors the `octo serve` CLI
	// path so both backends resolve the same set of environment variables.
	if profile.AllowEnvironmentModelSource {
		serveenv.Load()
	}

	// Pick the language for native dialogs/tray from the system UI language.
	applyLang()

	// ── Boot chain (P2) ──
	// Fail fast with a readable FatalDialog instead of a panic to a black
	// window. checkWebView2 / checkDataRoot / checkSingleInstance / checkPort
	// run before the app object exists; a failure is recorded here and surfaced
	// as a dialog in the ApplicationStarted hook (the Wails dialog needs the
	// event loop). See bootstrap.go.
	var bootFailure atomic.Pointer[BootFailure]
	if f := checkWebView2(); f != nil {
		bootFailure.Store(f)
	} else if f := checkDataRoot(); f != nil {
		bootFailure.Store(f)
	} else {
		// A same-directory relaunch (instance.json pid alive) is resolved by the
		// Wails single-instance mutex inside app.New: the acquire fails, the
		// first instance is notified and this process exits. Skip the port probe
		// in that case — the already-running instance owns 8088, so probing would
		// wrongly report "port in use" instead of activating the window. A port
		// probe failure here is *not* fatal yet: it may be this product's other
		// copy (a different directory), which app.New turns into notify+exit, so
		// the dialog is deferred to ApplicationStarted (see below).
		if activateExisting, _ := checkSingleInstance(); !activateExisting {
			if f := checkPort(hubAddr); f != nil {
				bootFailure.Store(f)
			}
		}
	}

	settings := loadDesktopSettings()

	// Seed the data-root bin/uv from the app's bundled copy on first run so skills
	// that need Python work even for a standalone download (no installer).
	ensureBundledUv()

	// Seed the octo CLI to ~/.local/bin (macOS + Linux) so a terminal has `octo`,
	// and on macOS put that dir on PATH. May update settings.SeededOctoVersion, so
	// it runs before the bridge takes its copy of settings below.
	ensureBundledOcto(&settings)

	bridge := &nativeBridge{settings: settings, url: desktopWebviewURL(), windowToken: newWindowToken()}
	// On Windows/Linux a window close would otherwise quit the app; start with
	// quit allowed only when the user opted out of keep-running-in-background.
	bridge.allowQuit.Store(!settings.KeepRunningInBackground)

	// Native notifications where the platform supports them (see
	// notificationsAvailable — macOS requires a bundle, Windows/Linux don't).
	// Registered as a Wails service so its ServiceStartup runs; the bridge holds
	// it to send notifications the frontend and the tray update check request.
	var services []application.Service
	if notificationsAvailable() {
		notifier := notifications.New()
		bridge.notifier = notifier
		services = append(services, application.NewService(notifier))
	}

	// The product name reaches the native shell from branding/brand.json rather
	// than a literal here, so renaming the product is a configuration edit. The
	// Wails Name/Description are OS-rendered metadata (macOS menu bar app name,
	// Windows taskbar) and thus class-B fixed values — the English full name,
	// not the UI-localized one, exactly like CFBundleName in Info.plist. The
	// tagline is the one-line product description, a closer fit for this field
	// than the paragraph-long Description. The single-instance UniqueID is class
	// C and comes from identifiers; it is a runtime lock name, so changing it is
	// what lets this product run beside an installed Octo rather than being
	// brought to its window.
	cfg := brand.Load()

	// The single-instance mutex (UniqueID) is shared by every portable copy on
	// the machine, so it cannot tell two copies apart. The second instance
	// sends its own executable path as AdditionalData; on the receiving side we
	// only raise our window when that path matches ours — a different
	// directory's launch is a different copy and must be ignored. The
	// directory-level instance.json (checkSingleInstance) already covers the
	// same-directory relaunch case, so this guard is purely defensive.
	selfExe := selfExePath()
	app := application.New(application.Options{
		Name:        cfg.Name(brand.DefaultLocale),
		Description: cfg.Tagline(brand.DefaultLocale),
		Services:    services,
		SingleInstance: &application.SingleInstanceOptions{
			UniqueID:       cfg.Identifier(brand.IdentifierSingleInstanceID),
			AdditionalData: map[string]string{"exe": selfExe},
			OnSecondInstanceLaunch: func(d application.SecondInstanceData) {
				if d.AdditionalData["exe"] == selfExe {
					bridge.showWindow()
					return
				}
				// A launch from a *different* directory: raise this window and
				// tell the user this copy is already running (需求 §5.1.2-5) —
				// otherwise they'd think a second copy opened.
				bridge.showWindow()
				bridge.showAlreadyRunning()
			},
		},
		// ShouldQuit is consulted on every termination attempt. On Windows/Linux
		// that includes closing the last window, so returning allowQuit there
		// keeps the hub alive in the tray (reopen via "Show Octo" or relaunch)
		// until the user picks "Quit Octo". macOS never quits on window close
		// thanks to the option below and handles real quits (Cmd-Q) itself, so
		// it always allows the quit.
		ShouldQuit: func() bool {
			if runtime.GOOS == "darwin" {
				return true
			}
			// A confirmed update restart quits so the updater's helper (which
			// waits for this process to exit) can swap the binary. Vetoing
			// that quit (keep-running-in-background) would deadlock the
			// update. Keyed on the user's restart action, not on updater
			// state: a staged-but-deferred update ("remind me later") must
			// not quietly disable keep-running-in-background.
			if bridge.updateRestart.Load() {
				return true
			}
			return bridge.allowQuit.Load()
		},
		Mac: application.MacOptions{
			// Closing the window must not quit the hub when the user wants it to
			// keep serving other clients in the background; the tray keeps it
			// reachable. When they opt out, last-window-close quits as usual.
			ApplicationShouldTerminateAfterLastWindowClosed: !settings.KeepRunningInBackground,
		},
		Windows: application.WindowsOptions{
			// Wails' Windows backend posts a quit message the moment its window
			// map empties (unregisterWindow), without consulting ShouldQuit —
			// so closing the window terminated the whole hub even when the user
			// asked it to keep running in the tray, and it also raced the
			// webview revive's window swap. Suppress that and let octo's own
			// ShouldQuit be the only authority, as it already is on mac/Linux;
			// the close path quits explicitly when the user opted out of
			// background running (see closeShouldQuit).
			DisableQuitOnLastWindowClosed: true,
			// Chromium's native-window occlusion tracker stops rendering a
			// WebView2 whose window is minimised or fully covered; on some
			// machines the compositor never paints again after restore,
			// leaving a permanently black window (WebView2Feedback#5171).
			// Disable the tracker so a backgrounded window keeps its render
			// pipeline alive. Wails appends this to its own disabled-feature
			// defaults. The cost is a still-rendering hidden webview, which
			// is acceptable for a tray-resident hub.
			DisabledFeatures: []string{"CalculateNativeWinOcclusion"},
		},
	})
	bridge.app = app

	// Configure the in-place updater when this build can swap itself (bundled
	// release build — see canInplaceUpdate). The flag routes the tray item and
	// the update toast through app.Updater instead of the download page.
	bridge.inplaceUpdate.Store(canInplaceUpdate() && initInplaceUpdater(app))

	// The updater window's Restart action quits the app so the helper can
	// swap the binary — flag it for ShouldQuit above. Registered here, before
	// any updater session opens its own (goroutine-spawning) restart handler,
	// so the flag is set by the time the quit is dispatched.
	app.Event.On(updater.EventUserRestart, func(*application.CustomEvent) {
		bridge.updateRestart.Store(true)
	})

	// Interacting with the "update available" toast starts the update flow;
	// session notifications focus and route to the relevant session; every other
	// notification just raises the window. Match the category (which all three
	// platform notifiers echo back) and then only the "Open" action or a tap
	// on the body — so dismissing the toast never triggers an action.
	if bridge.notifier != nil {
		bridge.notifier.OnNotificationResponse(func(res notifications.NotificationResult) {
			if res.Response.CategoryID == updateNotifyCategoryID {
				switch res.Response.ActionIdentifier {
				case updateNotifyOpenActionID, notifications.DefaultActionIdentifier:
					go startUpdateFlow(bridge)
				}
				return
			}
			if sid, ok := res.Response.UserInfo["session_id"].(string); ok && sid != "" {
				bridge.showWindowAt("chat/" + url.PathEscape(sid))
				return
			}
			bridge.showWindow()
		})
	}

	// System tray: reach the window or fully quit without hunting for the dock
	// icon. Quit goes through requestQuit so it can warn when stopping the hub
	// would disconnect other clients. An icon is required — a status item with
	// neither icon nor label doesn't render on macOS.
	tray := app.SystemTray.New()
	bridge.tray.Store(tray) // let update checks refresh the menu on their own goroutine
	if runtime.GOOS == "darwin" {
		tray.SetTemplateIcon(trayTemplateIcon)
	} else {
		tray.SetIcon(trayColorIcon)
	}
	tray.SetTooltip(L().takeoverTitle)
	tray.SetMenu(buildTrayMenu(app, bridge))
	// Keep the tray's status lines (backend, channels, connected clients) fresh
	// while the app runs — macOS doesn't refresh a status menu on open.
	go refreshTrayLoop(app, tray, bridge)

	// Bind + serve + open the window once the event loop is up, so the takeover
	// prompt (a modal dialog) can run. Doing it here rather than before Run lets
	// us ask the user before stopping someone else's backend.
	app.Event.OnApplicationEvent(events.Common.ApplicationStarted, func(*application.ApplicationEvent) {
		// A boot failure (WebView2 missing, data root unwritable, or the port
		// owned by another program) surfaces as a FatalDialog and the app stops
		// before any normal-startup side effect. A relaunch never reaches here:
		// app.New's single-instance mutex already notified the first instance
		// and exited this process.
		if f := bootFailure.Load(); f != nil {
			fatalDialog(app, bridge, f)
			return
		}
		// Record this process as the data root's owner before serving, so a
		// same-directory relaunch (checkSingleInstance) and the watchdog's thaw
		// can both confirm it's still us. Best-effort: a write failure degrades
		// to "no owner recorded" rather than blocking startup.
		_ = writeInstanceJSON(selfExe, os.Getpid())
		// Register the update-toast action category once the notification
		// service has started (Windows/Linux drop the action buttons if a
		// notification is sent before its category is registered).
		bridge.registerUpdateNotifyCategory()
		// Prompt for notification permission (macOS blocks until answered, so
		// off the UI thread) — without it every toast silently no-ops.
		go bridge.requestNotificationAuthorization()
		startHub(app, bridge, settings, profile)
	})

	// macOS: clicking the dock icon after the window was closed (hidden to the
	// tray) fires "reopen" — re-create/show the window instead of no-op'ing.
	app.Event.OnApplicationEvent(events.Mac.ApplicationShouldHandleReopen, func(*application.ApplicationEvent) {
		bridge.showWindow()
	})

	err := app.Run()

	// The app has quit: release our pid-file entry (only if it's still ours —
	// a successor that took the port over must keep its own), remove the
	// portable ownership marker, stop the data-root watchdog, and shut the
	// server down cleanly.
	serveproc.ReleaseOwned(os.Getpid())
	removeInstanceJSON()
	if wd := bridge.watchdog.Load(); wd != nil {
		wd.Stop()
	}
	if srv := bridge.srv.Load(); srv != nil {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		_ = srv.Shutdown(ctx)
		cancel()
	}
	if closeLog := bridge.closeLog.Load(); closeLog != nil {
		(*closeLog)()
	}
	if err != nil {
		log.Fatalf("octo-desktop: %v", err)
	}
}

// setupCrashLog redirects the process's stderr to the data-root crash.log so a panic
// that kills the app leaves a trace behind. Best-effort: if it fails there is
// nowhere to report that failure to (that being the whole problem), so the app
// starts anyway.
func setupCrashLog() {
	// Started from a terminal (a developer running the binary directly): that
	// terminal is a better place for a crash than a file nobody is tailing, and
	// taking stderr away would leave them staring at a silent window. The
	// shipped app never gets here — a -H windowsgui build has no console even
	// when launched from one, and a Finder/launchd launch has no terminal.
	if isatty.IsTerminal(os.Stderr.Fd()) {
		return
	}
	path, err := serveproc.CrashLogPath()
	if err != nil {
		return
	}
	banner := fmt.Sprintf("octo-desktop %s (%s/%s)", version.Version, runtime.GOOS, runtime.GOARCH)
	_ = crashlog.Install(path, banner)
}

// setupHubLog routes slog and the stdlib logger to a self-rotating
// data/logs/serve.log and returns a close func (nil if setup failed, leaving the
// default stderr in place). The stdlib logger is redirected too so the channel
// adapters' error/retry lines — still on `log` — land in the same file.
func setupHubLog() func() {
	logPath, err := serveproc.LogPath()
	if err != nil {
		return nil
	}
	lw, err := logfile.Open(logPath, logfile.DefaultMaxBytes, logfile.DefaultBackups)
	if err != nil {
		return nil
	}
	slog.SetDefault(slog.New(slog.NewTextHandler(lw, &slog.HandlerOptions{Level: hubLogLevel()})))
	log.SetOutput(lw)
	return func() { _ = lw.Close() }
}

// hubLogLevel reads OCTO_LOG_LEVEL (debug|info|warn|error), defaulting to info —
// matching `octo serve`'s level handling so the two backends behave alike.
func hubLogLevel() slog.Level {
	switch strings.ToLower(strings.TrimSpace(os.Getenv("OCTO_LOG_LEVEL"))) {
	case "debug":
		return slog.LevelDebug
	case "warn", "warning":
		return slog.LevelWarn
	case "error":
		return slog.LevelError
	default:
		return slog.LevelInfo
	}
}

// startHub takes ownership of the loopback port (offering to take over a running
// daemon), starts the in-process server, and opens the window. It runs inside
// the ApplicationStarted hook so its dialogs have a live event loop.
func startHub(app *application.App, bridge *nativeBridge, settings desktopSettings, profile productprofile.Profile) {
	// If another backend already owns the port, ask before displacing it.
	tookOver := false
	if pid, ok := serveproc.Running(); ok {
		if !bridge.confirmTakeover(pid) {
			app.Quit()
			return
		}
		if _, err := serveproc.Stop(); err != nil {
			bridge.showError(L().errTitle, fmt.Sprintf(L().errStopFmt, err))
			app.Quit()
			return
		}
		tookOver = true
	}

	// After a takeover, the stopped daemon needs a moment to release the port —
	// serveproc.Stop only signals it. Retry the bind for a few seconds so the
	// handoff is seamless; a cold start with a genuine conflict fails at once.
	grace := time.Duration(0)
	if tookOver {
		grace = 8 * time.Second
	}
	ln, err := listenHub(hubAddr, grace)
	if err != nil {
		bridge.showError(L().errTitle, fmt.Sprintf(L().errBindFmt, hubAddr, err))
		app.Quit()
		return
	}
	if path, perr := serveproc.PidPath(); perr == nil {
		_ = serveproc.WritePid(path, os.Getpid())
	}

	// Only now, having taken over any prior daemon and bound the port, are we the
	// sole backend — so it's safe to open the shared data/logs/serve.log. A prior
	// `octo serve -d` that was stopped above has since exited (listenHub only
	// succeeds once the port is free), releasing the fd it held on the file;
	// opening/rotating earlier (e.g. in main, before the takeover) could rotate a
	// file a live daemon still holds open — on Windows the rename would fail
	// outright and drop us to a console for the whole session. Set up before
	// server.New so the hub's own startup logs are captured too.
	if closeLog := setupHubLog(); closeLog != nil {
		bridge.closeLog.Store(&closeLog)
	}

	// Also now-we're-the-sole-backend: age out old upload/attachment files
	// (#2004). The desktop hub never goes through cmd/octo's runServe, so
	// without this call here it would silently never run — this IS the
	// long-running "desktop hub" instance the housekeeping is for.
	server.StartUploadsHousekeeping()

	srv, err := server.New(server.Config{
		Tools: profile.Startup.Tools,
		// P0-00A: a production Profile does not remove existing channel/tool
		// behavior. Product policy later controls visibility and authorization;
		// hiding a menu must never become a destructive feature toggle.
		NoChannel: !profile.Startup.Channels,
		// On: the version badge needs the latest-release lookup to know an update
		// exists. It reports upgrade_mode "installer" (Native is set), so the web
		// UI offers a download link; the desktop shell's own in-place update flow
		// lives in the tray + update toast (see startUpdateFlow), not the badge.
		UpdateCheck: true,
		Native:      bridge,
		// The desktop server runs in-process — there is no supervisor to
		// respawn it after a restart, so the restart_server tool would just
		// shut the HTTP server down and leave the GUI window attached to a
		// dead backend. Omit the tool; the desktop shell owns its own update
		// lifecycle (Check for Updates → installer).
		DisableRestart: true,
		// The product gate keys on this in-memory token to distinguish the
		// desktop window's requests from other loopback peers (CLI, VS Code,
		// Obsidian). Empty under `octo serve`, where nothing is gated.
		WindowToken: bridge.windowToken,
	})
	if err != nil {
		bridge.showError(L().errTitle, fmt.Sprintf(L().errStartFmt, err))
		app.Quit()
		return
	}
	bridge.srv.Store(srv)
	// Watch the portable data root from here: only after we own the port and
	// hold the server does a "data/ vanished" mean "freeze the product" rather
	// than "still booting". See watchdog.go.
	startDataRootWatchdog(bridge)
	go func() {
		if err := srv.ServeOn(ln); err != nil {
			log.Printf("octo-desktop: server stopped: %v", err)
		}
	}()

	bridge.showWindow()
}

// startDataRootWatchdog begins watching the portable data root and freezes the
// product when the directory disappears (a U盘 pulled out). The freeze/thaw is
// broadcast to the frontend over WS — the only channel an octo-served page has
// to the Go process — so the UI can show a full-screen frozen overlay. See
// watchdog.go and P2-启动与生命周期.md §3.4.
func startDataRootWatchdog(bridge *nativeBridge) {
	root, err := datapath.Root()
	if err != nil {
		return // no data root → nothing to watch (the boot chain already caught this)
	}
	wd := StartWatchdog(root,
		func() {
			if srv := bridge.srv.Load(); srv != nil {
				srv.BroadcastEvent(map[string]any{"type": "datastore:lost"})
			}
		},
		func() {
			if srv := bridge.srv.Load(); srv != nil {
				srv.BroadcastEvent(map[string]any{"type": "datastore:restored"})
			}
		},
	)
	bridge.watchdog.Store(wd)
}

// checkForUpdates is the tray "Check for updates…" action — a manual check that
// always reports its result.
func checkForUpdates(bridge *nativeBridge) { runUpdateCheck(bridge, true) }

// autoUpdateLoop checks for a newer release on its own, so the tray can show one
// without the user asking. A delayed first check keeps startup uncontended, then
// a daily cadence. Auto checks are silent unless they turn up a new version, and
// even then only when it differs from the one already surfaced — the tray item,
// not a daily toast, is the standing reminder.
func autoUpdateLoop(bridge *nativeBridge) {
	time.Sleep(30 * time.Second)
	runUpdateCheck(bridge, false)
	t := time.NewTicker(24 * time.Hour)
	defer t.Stop()
	for range t.C {
		runUpdateCheck(bridge, false)
	}
}

// runUpdateCheck performs one update lookup and records the outcome on the
// bridge so the tray can show a persistent, clickable "download" item — the
// durable signal, since macOS suppresses the toast while the app is foreground
// (exactly when a manual check runs). It runs on a background goroutine (never
// the UI thread) so the network round-trip can't freeze the menu.
//
// manual checks always report via an OS toast (failure, already-current, or the
// actionable "update available" toast whose button and body tap open the
// download page). auto checks stay silent except when they surface a version
// not already shown, so the daily cadence doesn't nag. Toasts are best-effort —
// on a build without the notification service (an unbundled macOS binary) they
// no-op, matching the version badge's own silence there.
func runUpdateCheck(bridge *nativeBridge, manual bool) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	latest, err := upgrade.Check(ctx)
	if err != nil {
		if manual {
			bridge.Notify(L().updTitle, L().updFailed)
		}
		return
	}
	current := strings.TrimPrefix(version.Version, "v")
	// Eligible() != nil means a dev/unbundled build that never claims to be
	// behind (matching the badge); report status without offering a download.
	if upgrade.Eligible() != nil || upgrade.CompareVersions(current, latest) >= 0 {
		bridge.updateAvailable.Store(nil)
		bridge.refreshTray()
		if manual {
			bridge.Notify(L().updTitle, fmt.Sprintf(L().updLatestFmt, current))
		}
		return
	}
	prev := bridge.updateAvailable.Load()
	bridge.updateAvailable.Store(&latest)
	bridge.refreshTray()
	if manual || prev == nil || *prev != latest {
		bridge.NotifyUpdateAvailable(L().updTitle, fmt.Sprintf(L().updAvailableFmt, latest))
	}
}

// listenHub binds addr, retrying for up to grace so a just-stopped daemon has
// time to release the port (SIGTERM only signals it; the listener closes a
// beat later). grace of 0 means a single attempt.
func listenHub(addr string, grace time.Duration) (net.Listener, error) {
	deadline := time.Now().Add(grace)
	for {
		ln, err := net.Listen("tcp", addr)
		if err == nil {
			return ln, nil
		}
		if time.Now().After(deadline) {
			return nil, err
		}
		time.Sleep(200 * time.Millisecond)
	}
}

// trayStatusLines is the (info-only) top of the tray menu: what the hub is
// doing right now — where it's serving and how many clients are attached.
func trayStatusLines(bridge *nativeBridge) []string {
	srv := bridge.srv.Load()
	if srv == nil {
		return []string{L().trayStarting}
	}
	lines := []string{fmt.Sprintf(L().trayBackendFmt, hubAddr)}
	lines = append(lines, fmt.Sprintf(L().trayClientsFmt, srv.ConnectedClients()))
	// Only when > 0 — keeps the menu clean before any channel is configured.
	if n := srv.ConfiguredChannelCount(); n > 0 {
		lines = append(lines, fmt.Sprintf(L().trayChannelsFmt, n))
	}
	return lines
}

// buildTrayMenu assembles the tray menu: disabled status lines on top, then the
// Show/Quit actions. Rebuilt (not mutated in place) so a refresh is one
// SetMenu call, which Wails marshals to the UI thread.
func buildTrayMenu(app *application.App, bridge *nativeBridge) *application.Menu {
	m := app.NewMenu()
	for _, line := range trayStatusLines(bridge) {
		m.Add(line).SetEnabled(false)
	}
	m.AddSeparator()
	m.Add(L().trayShow).OnClick(func(*application.Context) { bridge.showWindow() })
	m.Add(L().trayNewSession).OnClick(func(*application.Context) { bridge.openNewSession() })
	m.Add(L().traySettings).OnClick(func(*application.Context) { bridge.openSettings() })
	// OCTO-FORK: no "Check for updates" / "Update to vX" item — the portable
	// product has no in-place update (canInplaceUpdate is always false) and no
	// auto-update loop (需求 §5.1.2-13). The update flow stays reachable code
	// upstream, just not surfaced in the tray here.
	m.AddSeparator()
	// The USB-unplug reminder: a portable app runs from removable media, so a
	// quit must precede pulling the drive (需求 §5.1.2-8, P2 §3.6). A disabled
	// line like the status block — it is a hint, not an action.
	m.Add(L().trayUnplugHint).SetEnabled(false)
	m.Add(L().trayQuit).OnClick(func(*application.Context) { bridge.requestQuit() })
	return m
}

// refreshTrayLoop re-publishes the tray menu whenever its status text changes,
// so the counts stay live without rebuilding on every tick.
func refreshTrayLoop(app *application.App, tray *application.SystemTray, bridge *nativeBridge) {
	sigOf := func() string {
		applyLang() // follow a language switch made in onboarding / Settings
		// The status lines are language-dependent, so a language switch changes
		// this signature and triggers a rebuild (which re-reads L() for labels).
		// The update state is folded in too, so a flip rebuilds even if the
		// immediate refreshTray call raced with a concurrent rebuild.
		upd := ""
		if v := bridge.updateAvailable.Load(); v != nil {
			upd = *v
		}
		return strings.Join(trayStatusLines(bridge), "|") + "\x00" + upd
	}
	last := sigOf()
	t := time.NewTicker(3 * time.Second)
	defer t.Stop()
	for range t.C {
		sig := sigOf()
		if sig == last {
			continue
		}
		last = sig
		tray.SetMenu(buildTrayMenu(app, bridge))
	}
}

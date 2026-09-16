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
	_ "embed"
	"fmt"
	"log"
	"log/slog"
	"net"
	"net/url"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	"github.com/mattn/go-isatty"
	"github.com/open-octo/octo-agent/internal/brand"
	"github.com/open-octo/octo-agent/internal/crashlog"
	"github.com/open-octo/octo-agent/internal/datapath"
	"github.com/open-octo/octo-agent/internal/logfile"
	"github.com/open-octo/octo-agent/internal/productprofile" // OCTO-FORK: gateway model prefix (§PR-4c0)
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

// hubPort is the fixed loopback port the hub owns — the same default
// `octo serve` binds, so every existing client (Web, VS Code, Obsidian, CLI)
// finds it without configuration. LAN exposure stays a CLI concern.
//
// Split out from hubAddr because 需求20260906 §5.1.2 第 6 条 prescribes a
// port-conflict message that names the port ("端口 8088 已被占用") while the
// bind error names the address. Both readings must come off one number, or the
// message and the socket could disagree.
const hubPort = "8088"

// hubAddr is the fixed loopback address the hub owns.
const hubAddr = "127.0.0.1:" + hubPort

// minFreeBytes is the room the data root must have before this product is worth
// starting (需求20260906 §5.1.2 第 4 条's 空间不足 arm).
//
// The figure is derived, not chosen: the rotating logs' worst-case on-disk
// footprint — the one number in the tree that already describes how much room
// this program needs just to keep running (see internal/logfile's DefaultMaxBytes
// /DefaultBackups, 10 MiB × 4). Below it the log writer starts discarding its own
// history immediately and every write is racing a full volume.
//
// It is deliberately small. The check exists to catch a volume that cannot
// serve this product at all, not to enforce a data-retention policy — a product
// that refuses to start because a disk is "only" 1 GiB free would be worse than
// the failure it prevents. Whether the floor should be higher (to leave room for
// uploads and sessions) is a product call, registered alongside V-82.
var minFreeBytes = uint64(logfile.DefaultMaxBytes * (logfile.DefaultBackups + 1))

// humanBytes renders a byte count for a dialog: one decimal, in the largest
// unit that keeps the number readable. Exact values are pointless here — the
// user is deciding whether to free up space, not auditing a filesystem.
func humanBytes(n uint64) string {
	const unit = 1024
	if n < unit {
		return fmt.Sprintf("%d B", n)
	}
	value := float64(n)
	units := []string{"KiB", "MiB", "GiB", "TiB"}
	for _, suffix := range units {
		value /= unit
		if value < unit {
			return fmt.Sprintf("%.1f %s", value, suffix)
		}
	}
	return fmt.Sprintf("%.1f PiB", value/unit)
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
	serveenv.Load()

	// Pick the language for native dialogs/tray from the system UI language.

	// Pick the language for native dialogs/tray from the system UI language.
	applyLang()

	settings := loadDesktopSettings()

	// Seed the data-root bin/uv from the app's bundled copy on first run so skills
	// that need Python work even for a standalone download (no installer).
	ensureBundledUv()

	// Seed the octo CLI to ~/.local/bin (macOS + Linux) so a terminal has `octo`,
	// and on macOS put that dir on PATH. May update settings.SeededOctoVersion, so
	// it runs before the bridge takes its copy of settings below.
	ensureBundledOcto(&settings)

	bridge := &nativeBridge{settings: settings, url: "http://" + hubAddr}
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
	app := application.New(application.Options{
		Name:        cfg.Name(brand.DefaultLocale),
		Description: cfg.Tagline(brand.DefaultLocale),
		Services:    services,
		SingleInstance: &application.SingleInstanceOptions{
			UniqueID: cfg.Identifier(brand.IdentifierSingleInstanceID),
			// OCTO-FORK: publish the launching copy's own program directory, so a
			// second launch from a different folder can be told from a repeat one
			// (V-83) — see second_instance.go for why AdditionalData is the only
			// channel that carries it.
			AdditionalData: publishProgramDir(),
			OnSecondInstanceLaunch: func(data application.SecondInstanceData) {
				// OCTO-FORK: 需求20260906 §5.1.2 第 5 条 — a second launch from
				// another program directory is not the same event as double-clicking
				// this one, and must not be silently turned into "your window is
				// back" (V-83).
				ours, _ := datapath.ProgramDir()
				if secondInstanceActionFrom(data, ours) == warnOtherCopy {
					bridge.warnOtherCopy()
					return
				}
				bridge.showWindow()
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

	// OCTO-FORK: 便携交付物不做更新：原地写回整块不装配，于是 inplaceUpdate 恒为 false，
	// 即便有路径调到 startUpdateFlow 也只会打开下载页、不会自我替换（需求 §5.1.2 第 13 条；
	// 理由见 update.go 的 productUpdatesEnabled）。上游这段代码留在原地，只是到不了。
	// — see dev-docs-usdable/需求/2260906/技术方案/P2-启动与生命周期.md §5（V-86）
	if productUpdatesEnabled {
		// Configure the in-place updater when this build can swap itself (bundled
		// release build — see canInplaceUpdate). The flag routes the tray item and
		// the update toast through app.Updater instead of the download page.
		bridge.inplaceUpdate.Store(canInplaceUpdate() && initInplaceUpdater(app))
	}

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
		// Register the update-toast action category once the notification
		// service has started (Windows/Linux drop the action buttons if a
		// notification is sent before its category is registered).
		bridge.registerUpdateNotifyCategory()
		// Prompt for notification permission (macOS blocks until answered, so
		// off the UI thread) — without it every toast silently no-ops.
		go bridge.requestNotificationAuthorization()
		startHub(app, bridge, settings)
		// OCTO-FORK: 便携交付物不做更新：定时自动检查整块不启动（需求 §5.1.2 第 13 条；
		// 理由见 update.go 的 productUpdatesEnabled）。上游这个 goroutine 留在原地，只是
		// 永远不被启动 —— 没有它就没有"新版本"通知，托盘的更新项也随之没有来源。
		// — see dev-docs-usdable/需求/2260906/技术方案/P2-启动与生命周期.md §5（V-86）
		if productUpdatesEnabled {
			// Surface a newer release in the tray without the user asking: a delayed
			// first check, then daily. Foreground-suppressed toasts don't matter here
			// — the tray item is the durable signal.
			go autoUpdateLoop(bridge)
		}
	})

	// macOS: clicking the dock icon after the window was closed (hidden to the
	// tray) fires "reopen" — re-create/show the window instead of no-op'ing.
	app.Event.OnApplicationEvent(events.Mac.ApplicationShouldHandleReopen, func(*application.ApplicationEvent) {
		bridge.showWindow()
	})

	err := app.Run()

	// OCTO-FORK: stop the data-root watchdog before the process tears down — see
	// watchdog.go and dev-docs-usdable/需求/20260911/开发计划0911/ 的 L-E3.
	if wd := bridge.watchdog.Load(); wd != nil {
		wd.Stop()
	}

	// The app has quit: release our pid-file entry (only if it's still ours —
	// a successor that took the port over must keep its own) and shut the
	// server down cleanly.
	serveproc.ReleaseOwned(os.Getpid())
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
func startHub(app *application.App, bridge *nativeBridge, settings desktopSettings) {
	// L-E3: an unusable data root means there is nothing this process can
	// safely do. Every path it writes goes through internal/datapath, which
	// refuses rather than falling back to a host directory (P1 §3.2) — so the
	// product would come up with a window and no working storage. Fail closed
	// here, before any of the launch side effects below, and in particular
	// before the takeover prompt: stopping a running daemon on behalf of a
	// process that is about to quit would take a working backend down for
	// nothing (§4.2: 启动时数据根不可用 = fail-closed，不进主界面).
	//
	// The %v carries datapath's own reason, so the user learns the cause: the
	// probe distinguishes "the path is not there" from "it is there but not
	// writable" (需求20260906 §5.1.2 第 4 条).
	// OCTO-FORK: portable-delivery boot gate — see
	// dev-docs-usdable/需求/20260911/开发计划0911/ 的 L-E3. The free-space half
	// below is V-82.
	root, err := datapath.Root()
	if err != nil {
		bridge.showError(L().errTitle, fmt.Sprintf(L().errStartFmt, err))
		app.Quit()
		return
	}

	// V-82: a writable but nearly-full volume passes the probe above and then
	// fails later, mid-session, as a write error that reads like an unrelated
	// fault (需求20260906 §5.1.2 第 4 条). Refuse to start instead, and say how
	// much room there is — the user cannot act on "not enough space" without it.
	//
	// A failure to *measure* is not treated as a failure to start: the
	// directory was just created and probed successfully, so the volume is
	// there, and refusing to come up because a statistic could not be read
	// inverts the point of the check (bounded degradation, 开发规范 §3.9 —
	// target: no space pre-check at all; the user is not told; it recovers as
	// soon as the reading works, i.e. it is silent and one-sided by design).
	if free, err := datapath.FreeSpace(root); err == nil && free < minFreeBytes {
		bridge.showError(L().errTitle, fmt.Sprintf(L().errNoSpaceFmt, humanBytes(free), humanBytes(minFreeBytes)))
		app.Quit()
		return
	}

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
		// OCTO-FORK: 规定文案点名端口而非地址（V-84）—— 见 lang.go 的 errBindFmt。
		bridge.showError(L().errTitle, fmt.Sprintf(L().errBindFmt, hubPort, err))
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

	// One assembly, three seams (PR-5a + PR-5e): the local product API's routes,
	// the built-in gateway's sender factory, and the turn guard's catalog
	// predicate. Built together because they must share one CredentialHolder — the
	// gateway presents the token the platform client refreshed, so two holders
	// would mean it kept presenting a stale one — and because the predicate reads
	// the very catalog store whose routes are mounted here.
	//
	// Any of the first three can be nil, and that means "not wired in this
	// build" to internal/server rather than "no gateway": see mountProductAPI's
	// fail-closed branches and the nil-means-unchanged-upstream-behavior
	// contracts on GatewaySender and CatalogOffers.
	//
	// The engine is the fourth because the two sides that need it cannot import
	// each other (PR-6b1): internal/server masks model output, and
	// internal/productruntime answers the input-check and dictionary routes.
	// Assembling it once here is what keeps them on the same word list. It stays
	// non-nil when the product is not mounted (the server then builds its own),
	// which is the `octo serve` shape.
	//
	// The fifth is the input gate (PR-6b3). It travels as a value for the same
	// reason the engine does: the two facts it needs — the user's switch and the
	// engine — belong to internal/productstate and this assembly, and
	// internal/server must not hold either (it would have to import a fork
	// package, which the dependency direction forbids).
	mountProduct, gatewaySender, catalogOffers, engine, sensitiveInputGate := mountProductAPI()

	// Immutable for the life of the process, and the owner of both facts the
	// turn-path policy needs (see RequireGateway below), so it is read once.
	profile := productprofile.Current()

	srv, err := server.New(server.Config{
		Tools: true,
		// OCTO-FORK: 便携交付物不做更新：这里**必须**是 false，否则 GET /api/version 会去做
		// 一次出站的最新发布查询（internal/server 的 latestVersion 以本字段为开关），而
		// 打开完整设置就会调它 ⇒ 等于"自动检查更新"从 web 半边活着（需求 §5.1.2 第 13 条）。
		// false 时该端点退化为 latest==current、needs_update=false，全程零出站请求；设置页
		// 那处更新行同时换成了「即将支持」占位（SettingsModal，钉子见 web/src/lib/updateEntry.test.ts），
		// 于是界面上不会出现"已是最新版本"这种**没人查过却敢说**的结论（§3.9）。
		// 上游注释原写"On: the version badge needs the latest-release lookup" —— 那个徽章
		// （VersionBadge.svelte）在本仓**根本没有被挂载**，全仓只有它自己的测试引用它。
		UpdateCheck: productUpdatesEnabled,
		Native:      bridge,
		// The desktop server runs in-process — there is no supervisor to
		// respawn it after a restart, so the restart_server tool would just
		// shut the HTTP server down and leave the GUI window attached to a
		// dead backend. Omit the tool; the desktop shell owns its own update
		// lifecycle (Check for Updates → installer).
		DisableRestart: true,
		// OCTO-FORK: our product routes and the built-in gateway's sender, both
		// from one assembly so they share one credential holder — see
		// mountProductAPI and dev-docs-usdable/需求/20260911/开发计划.md §PR-5a.
		MountAPI: mountProduct,
		// OCTO-FORK: the product gate's window identity — see
		// dev-docs-usdable/需求/20260911/开发计划.md §PR-2b2b. Generated here
		// because this runs before the first window is shown, which is what lets
		// shellURL carry the token into that window's very first URL.
		WindowToken: windowToken(),
		// OCTO-FORK: gateway-bound models are served by the built-in gateway —
		// see dev-docs-usdable/需求/20260911/开发计划.md §PR-5a. The prefix is
		// what marks a model as the gateway's (PR-4c0); the factory is how a
		// gateway-bound turn is actually served (PR-5a). With both injected,
		// internal/server stops requiring a third-party endpoint at startup
		// (V-36), because such a build has none by design (B4).
		GatewayModelPrefix: productprofile.GatewayModelPrefix(),
		GatewaySender:      gatewaySender,
		// OCTO-FORK: the model source this build permits — see
		// dev-docs-usdable/需求/20260911/开发计划.md §PR-5c. Both values are read
		// from the profile (their owner) rather than decided here, and they are
		// what stops a production build from falling back to config.yml or the
		// environment when the gateway cannot serve a turn (C9 规则 2, L-C5).
		// A developer profile answers false to the first, so local models keep
		// working there.
		RequireGateway:    profile.RequiresControlPlane(),
		ControlPlaneReady: profile.ControlPlaneConfigured() && profile.HasTrustedKeys(),
		// OCTO-FORK: a session whose catalog model was withdrawn — see
		// dev-docs-usdable/需求/20260911/开发计划.md §PR-5e. The predicate is the
		// runtime's own, so "which models exist" keeps one owner; the shell only
		// forwards it.
		CatalogOffers: catalogOffers,
		// OCTO-FORK: the process's one compliance-word engine — see
		// dev-docs-usdable/需求/20260911/开发计划.md §PR-6b1. Forwarded, not
		// built here: the assembly above owns it, so the turn path and the
		// product routes cannot end up on two dictionaries. nil (a build with no
		// product) leaves internal/server to build its own.
		SensitiveEngine: engine,
		// OCTO-FORK: the server-side input gate (需求 D1, L-D1) — see
		// dev-docs-usdable/需求/20260911/开发计划.md §PR-6b3. Forwarded for the
		// same reason as the engine above: the verdict needs the user's switch
		// (productstate) and the engine, both owned by the assembly, and
		// internal/server must not hold either. nil (a build with no product, or
		// a test) means "no gate" — nothing is refused.
		SensitiveInputGate: sensitiveInputGate,
	})
	if err != nil {
		bridge.showError(L().errTitle, fmt.Sprintf(L().errStartFmt, err))
		app.Quit()
		return
	}
	bridge.srv.Store(srv)
	go func() {
		if err := srv.ServeOn(ln); err != nil {
			log.Printf("octo-desktop: server stopped: %v", err)
		}
	}()

	bridge.showWindow()

	// L-E3: arm the data-root watchdog. A portable product lives on media that
	// can be pulled out mid-session, and while the root is gone every write must
	// be refused — above all the creation of a fresh, empty data/ beside the
	// executable, which is indistinguishable from a clean install and would
	// present onboarding while the user's real data sat on the disconnected disk
	// (§4.2).
	//
	// The watchdog owns detection and flips datapath's process-wide gate, which
	// is what every directory-creating path already consults (Root, Sub) — so
	// this is a real write stop, not a frozen screen with writers still running.
	//
	// The callbacks do two separate jobs: a log line for the operator, and the
	// datastore:lost / datastore:restored event for the user. The user-visible
	// half of the freeze is owned elsewhere — App.svelte subscribes to those
	// events, a `frozen` store drives FrozenOverlay.svelte, and the copy is
	// written (i18n `frozen.title/desc`) — so nothing is worded here: a second
	// wording typed into the native dialogs would be a second owner of the same
	// message, going stale on the next edit (§3.8). The trigger, scope and
	// replay semantics were G1/N-5's to settle and were settled on 2026-09-16
	// (开发计划 §4.1 第 45 行): global, one event per transition, and replayed to
	// a window that connects while frozen (internal/server/product_events.go).
	//
	// Armed after the window is up so there is a running product to notify; a
	// root that is already unusable never reaches here (see the startup check
	// above, which fails closed instead).
	// OCTO-FORK: arm the portable data-root watchdog — see
	// dev-docs-usdable/需求/20260911/开发计划0911/ 的 L-E3.
	if root, err := datapath.Root(); err == nil {
		bridge.watchdog.Store(StartWatchdog(root, func() {
			slog.Error("the data root is gone; writes are frozen", "root", root)
			broadcastProductState(bridge, "datastore:lost")
		}, func() {
			slog.Info("the data root is back; writes are released", "root", root)
			broadcastProductState(bridge, "datastore:restored")
		}))
	}
}

// broadcastProductState tells the windows about a product-wide state change.
//
// Both ends of the lifetime are tolerated on purpose: the watchdog is armed
// while startup is still running, so the server may not be constructed yet, and
// it is stopped during shutdown, when the server may already be gone. Neither
// is worth a failure — the log line is the operator's record of the transition
// and the event is the user's, so one of them arriving late must not take the
// other with it.
func broadcastProductState(bridge *nativeBridge, eventType string) {
	if srv := bridge.srv.Load(); srv != nil {
		srv.BroadcastProductEvent(map[string]any{"type": eventType})
	}
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
	// OCTO-FORK: 便携交付物不做更新：托盘更新入口整块不挂（需求 §5.1.2 第 13 条；理由见
	// update.go 的 productUpdatesEnabled）。上游这两个分支留在原地，只是永远走不到 ——
	// 于是托盘上既没有"更新到 vX"也没有"检查更新…"，用户能看到的更新入口只剩个人中心里
	// 那个「即将支持」占位（AccountPanel，PQ12 规定的形态）。
	// — see dev-docs-usdable/需求/2260906/技术方案/P2-启动与生命周期.md §5（V-86）
	if productUpdatesEnabled {
		// A known-newer release replaces the "check" item with a one-click update
		// (in-place when this build supports it, else the download page) — the
		// durable prompt when the toast was suppressed. Otherwise the manual check.
		if v := bridge.updateAvailable.Load(); v != nil {
			m.Add(fmt.Sprintf(L().trayUpdateAvailFmt, *v)).OnClick(func(*application.Context) {
				go startUpdateFlow(bridge)
			})
		} else {
			m.Add(L().trayCheckUpdates).OnClick(func(*application.Context) { go checkForUpdates(bridge) })
		}
	}
	m.AddSeparator()
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

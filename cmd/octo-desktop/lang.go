package main

import (
	"os"
	"strings"
	"sync/atomic"

	"github.com/open-octo/octo-agent/internal/brand"
	"github.com/open-octo/octo-agent/internal/config"
)

// uiStrings holds every user-facing native string the desktop shell shows
// outside the web UI — dialogs, the tray menu, error messages. Selected once at
// startup by the system language. Format strings keep their verbs so call sites
// can fmt.Sprintf them.
type uiStrings struct {
	trayShow, trayQuit string
	traySettings       string
	trayNewSession     string
	trayCheckUpdates   string
	trayUpdateAvailFmt string // "↑ Update to v%s"
	trayStarting       string
	trayBackendFmt     string // "Backend · %s"
	trayClientsFmt     string // "Connected clients: %d"
	trayChannelsFmt    string // "Configured channels: %d"

	takeoverTitle  string
	takeoverMsgFmt string // "...(pid %d)..."
	takeoverOK     string
	takeoverCancel string

	quitTitle  string
	quitMsg    string
	quitOK     string
	quitCancel string

	errTitle     string
	errBindFmt   string // "...%s...%v"
	errStopFmt   string // "...%v"
	errStartFmt  string // "...%v"
	dialogOKText string

	updTitle         string
	updFailed        string
	updLatestFmt     string // "...(v%s)."
	updAvailableFmt  string // "...%s..."
	updOpen          string
	updInstall       string
	updInplaceFailed string

	// Boot-chain FatalDialog (bootstrap.go). Each maps one BootFailure reason;
	// the port one names the upstream product "Octo" as an external program.
	fatalQuit             string // the single dialog button
	fatalWebView2Download string // the WebView2 download-page button
	fatalWebView2         string
	fatalDataRootFmt      string // "...%s" (the failing path)
	fatalPortFmt          string // fixed copy; "Octo" in it names the upstream product
	fatalUnknownFmt       string // "...%s" (the reason)

	// Second-copy launch prompt (P2 §3.2): shown by the first instance when a
	// launch from a *different* directory arrives.
	alreadyRunningMsg string

	// Tray / quit "unplug" hints (P2 §3.6).
	trayUnplugHint string
}

// The product name is not written into these tables. It comes from
// internal/brand (branding/brand.json) via the constructors below, so renaming
// the product is a configuration edit rather than a sweep through every dialog.
//
// Constructors taking the name, rather than the {brand} placeholders the web
// dictionaries use: Go concatenates at construction, so a field that forgets
// the brand is a visibly missing parameter instead of a literal "{brand}"
// reaching a dialog for a test to catch. It also leaves the fmt verbs alone —
// a placeholder scheme riding on Sprintf would have to escape every %d and %v.
//
// Each language decides its own spacing, which is the point: English writes
// "Show " with a trailing space, Chinese writes "显示" without one, because CJK
// takes no space around an inline name.
func enStringsFor(name, short string) uiStrings {
	return uiStrings{
		trayShow:           "Show " + short,
		trayQuit:           "Quit " + short,
		trayNewSession:     "New Session",
		traySettings:       "Settings…",
		trayCheckUpdates:   "Check for Updates…",
		trayUpdateAvailFmt: "↑ Update to v%s",
		trayStarting:       "Starting…",
		trayBackendFmt:     "Backend · %s",
		trayClientsFmt:     "Connected clients: %d",
		trayChannelsFmt:    "Configured channels: %d",

		takeoverTitle:  name,
		takeoverMsgFmt: "A background " + name + " backend is already running (pid %d).\n\nStop it and run " + name + " as the hub for this machine?",
		takeoverOK:     "Stop and Continue",
		takeoverCancel: "Quit",

		quitTitle:  "Quit " + short,
		quitMsg:    "Quitting stops the " + name + " backend on this machine. Connected editors, browsers, and IM channels will disconnect.\n\nQuit before unplugging the USB drive.\n\nQuit anyway?",
		quitOK:     "Quit",
		quitCancel: "Cancel",

		errTitle:     name,
		errBindFmt:   "Couldn't bind %s — another program may be using it.\n\n%v",
		errStopFmt:   "Couldn't stop the running backend: %v",
		errStartFmt:  "Couldn't start the backend: %v",
		dialogOKText: "OK",

		updTitle:         name,
		updFailed:        "Couldn't check for updates. Please try again later.",
		updLatestFmt:     "You're on the latest version (v%s).",
		updAvailableFmt:  name + " %s is available.",
		updOpen:          "Open Download Page",
		updInstall:       "Update Now",
		updInplaceFailed: "Automatic update failed — opening the download page.",

		fatalQuit:             "Quit",
		fatalWebView2Download: "Download WebView2",
		fatalWebView2:         "This app needs the Microsoft WebView2 Runtime to show its window, but it isn't installed on this computer.\n\nPlease install WebView2, then start " + name + " again.",
		fatalDataRootFmt:      "The data directory couldn't be created or written to:\n\n%s\n\n" + name + " keeps all its data in a data/ folder next to the executable, which must be writable.",
		fatalPortFmt:          "Port 8088 is already in use. If Octo or another program is running on this computer, quit it first, then open " + name + " again.", // upstream product name, not our brand
		fatalUnknownFmt:       name + " couldn't start (%s).",

		alreadyRunningMsg: name + " is already running. Quit it before opening another copy.",

		trayUnplugHint: "Quit before unplugging the USB drive",
	}
}

func zhStringsFor(name, short string) uiStrings {
	return uiStrings{
		trayShow:           "显示" + short,
		trayQuit:           "退出" + short,
		trayNewSession:     "新建会话",
		traySettings:       "设置…",
		trayCheckUpdates:   "检查更新…",
		trayUpdateAvailFmt: "↑ 更新到 v%s",
		trayStarting:       "启动中…",
		trayBackendFmt:     "后端 · %s",
		trayClientsFmt:     "已连接客户端：%d",
		trayChannelsFmt:    "已配置 channel：%d",

		takeoverTitle:  name,
		takeoverMsgFmt: "已有一个" + name + "后端在后台运行（pid %d）。\n\n停止它，并让" + name + "作为本机的后端中枢？",
		takeoverOK:     "停止并继续",
		takeoverCancel: "退出",

		quitTitle:  "退出" + short,
		quitMsg:    "退出会停止本机的" + name + "后端，已连接的编辑器、浏览器和 IM channel 都会断开。\n\n退出后再拔出 U 盘。\n\n仍要退出？",
		quitOK:     "退出",
		quitCancel: "取消",

		errTitle:     name,
		errBindFmt:   "无法绑定 %s —— 可能有其他程序正在占用。\n\n%v",
		errStopFmt:   "无法停止正在运行的后端：%v",
		errStartFmt:  "无法启动后端：%v",
		dialogOKText: "好",

		updTitle:         name,
		updFailed:        "检查更新失败,请稍后重试。",
		updLatestFmt:     "已是最新版本(v%s)。",
		updAvailableFmt:  name + " %s 已发布。",
		updOpen:          "打开下载页",
		updInstall:       "立即更新",
		updInplaceFailed: "自动更新失败,已打开下载页。",

		fatalQuit:             "退出",
		fatalWebView2Download: "下载 WebView2",
		fatalWebView2:         "本程序需要 Microsoft WebView2 运行时才能显示窗口,但此电脑未安装。\n\n请先安装 WebView2,再重新打开" + name + "。",
		fatalDataRootFmt:      "数据目录无法创建或写入:\n\n%s\n\n" + name + "把所有数据保存在可执行文件旁的 data/ 目录,该目录需可写。",
		fatalPortFmt:          "端口 8088 已被占用。若本机正在运行 Octo 或其它程序,请先退出后再打开" + name + "。", // upstream product name, not our brand
		fatalUnknownFmt:       name + "无法启动(%s)。",

		alreadyRunningMsg: name + "已在运行，请先退出再打开另一份。",

		trayUnplugHint: "退出后再拔出 U 盘",
	}
}

// Built once at startup: the brand configuration is embedded in the binary, so
// there is nothing to re-read when the language changes.
var (
	enStrings = brandedStrings(enStringsFor, "en-US")
	zhStrings = brandedStrings(zhStringsFor, "zh-CN")
)

func brandedStrings(build func(name, short string) uiStrings, locale string) uiStrings {
	cfg := brand.Load()
	return build(cfg.Name(locale), cfg.ShortName(locale))
}

// active holds the current string set. It's an atomic pointer because the tray
// refresh loop re-applies the language on its own goroutine while dialog
// callbacks read it on the UI thread.
var active atomic.Pointer[uiStrings]

// L returns the active string set (English until applyLang runs).
func L() *uiStrings {
	if s := active.Load(); s != nil {
		return s
	}
	return &enStrings
}

// applyLang re-resolves the UI language and swaps the active string set. Called
// at startup and on each tray tick, so switching language in onboarding or
// Settings (which writes data/config.yml) is reflected in the tray/dialogs
// within a few seconds — the desktop shell follows the in-app language choice,
// not the OS.
func applyLang() {
	if resolveLang() == "zh" {
		active.Store(&zhStrings)
	} else {
		active.Store(&enStrings)
	}
}

// resolveLang prefers the user's in-app language (config.yml `language`, set in
// onboarding / Settings). Only before that's chosen does it fall back to the
// OS's preferred UI language.
func resolveLang() string {
	if cfg, err := config.Load(); err == nil {
		switch cfg.Language {
		case "zh":
			return "zh"
		case "en":
			return "en"
		}
	}
	return preferredLang()
}

// preferredLang returns "zh" or "en" from the OS's preferred UI language. It
// asks the platform (osLang: AppleLanguages on macOS, GetUserDefaultLocaleName
// on Windows) first, since a Finder/Explorer-launched app usually has no LANG;
// only when that yields nothing does it fall back to the LC_*/LANG environment
// (which is how Linux exposes it).
func preferredLang() string {
	if lang := osLang(); lang != "" {
		if strings.HasPrefix(strings.ToLower(lang), "zh") {
			return "zh"
		}
		return "en"
	}
	for _, k := range []string{"LC_ALL", "LC_MESSAGES", "LANG"} {
		if v := os.Getenv(k); v != "" {
			if strings.HasPrefix(strings.ToLower(v), "zh") {
				return "zh"
			}
			return "en"
		}
	}
	return "en"
}

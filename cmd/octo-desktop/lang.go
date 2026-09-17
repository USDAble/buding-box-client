package main

import (
	"os"
	"strings"
	"sync/atomic"

	// OCTO-FORK: 内置默认工作区从 `~/Octo` 改为数据根下的 `data/workspace/`（便携化） — see dev-docs-usdable/需求/2260906/技术方案/P12-便携打包.md
	"github.com/open-octo/octo-agent/internal/brand"
	"github.com/open-octo/octo-agent/internal/config"
)

// uiStrings holds every user-facing native string the desktop shell shows
// outside the web UI — dialogs, the tray menu, error messages. Selected once at
// startup by the system language. Format strings keep their verbs so call sites
// can fmt.Sprintf them.
type uiStrings struct {
	trayShow, trayQuit   string
	traySettings         string
	trayNewSession       string
	trayPet, trayPetHide string
	trayCheckUpdates     string
	trayUpdateAvailFmt   string // "↑ Update to v%s"
	trayStarting         string
	trayBackendFmt       string // "Backend · %s"
	trayClientsFmt       string // "Connected clients: %d"
	trayChannelsFmt      string // "Configured channels: %d"

	takeoverTitle  string
	takeoverMsgFmt string // "...(pid %d)..."
	takeoverOK     string
	takeoverCancel string

	quitTitle  string
	quitMsg    string
	quitOK     string
	quitCancel string

	errTitle      string
	errBindFmt    string // "%s" is the port, "%v" the bind error
	errStopFmt    string // "...%v"
	errStartFmt   string // "...%v"
	errNoSpaceFmt string // "%s" free, "%s" required

	// otherCopyTitle / otherCopyMsg are the "you opened a second copy" notice
	// (需求20260906 §5.1.2 第 5 条). The message is the prescribed sentence.
	otherCopyTitle string
	otherCopyMsg   string

	dialogOKText string

	updTitle         string
	updFailed        string
	updLatestFmt     string // "...(v%s)."
	updAvailableFmt  string // "...%s..."
	updOpen          string
	updInstall       string
	updInplaceFailed string
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
		// OCTO-FORK: upstream #2421 added the desktop pet; its two tray labels are
		// brand-free, so they land here verbatim from upstream's table — see
		// dev-docs-usdable/需求/20260911/开发计划.md §PR-3
		trayPet:     "Show Pet",
		trayPetHide: "Hide Pet",

		takeoverTitle:  name,
		takeoverMsgFmt: "A background " + name + " backend is already running (pid %d).\n\nStop it and run " + name + " as the hub for this machine?",
		takeoverOK:     "Stop and Continue",
		takeoverCancel: "Quit",

		quitTitle:  "Quit " + short,
		quitMsg:    "Quitting stops the " + name + " backend on this machine. Connected editors, browsers, and IM channels will disconnect.\n\nQuit anyway?",
		quitOK:     "Quit",
		quitCancel: "Cancel",

		errTitle: name,
		// 需求20260906 §5.1.2 第 6 条 prescribes this sentence, and its "Octo"
		// names the UPSTREAM product on purpose: the situation it describes is the
		// user having upstream Octo installed, so interpolating our brand here
		// would tell them to quit the wrong program. The explicit exception 硬规则 2
		// grants, cashed in by the marker below.
		// brand-exception: 第 6 条规定文案点名上游产品
		errBindFmt:  "Port %s is already in use. If Octo or another program is running on this computer, quit it and open " + name + " again.\n\n%v",
		errStopFmt:  "Couldn't stop the running backend: %v",
		errStartFmt: "Couldn't start the backend: %v",
		// V-82: names the reason and the two figures, because "not enough space"
		// on its own gives the user nothing to act on.
		errNoSpaceFmt: "Not enough room to run. The data folder is on a disk with %s free, and %s is needed.\n\nFree up space, then open " + name + " again.",

		// V-83: the prescribed sentence, in the English table's voice. It names the
		// product because the user may have two folders on screen at once.
		otherCopyTitle: name,
		otherCopyMsg:   name + " is already running. Quit it first, then open the other copy.",
		dialogOKText:   "OK",

		updTitle:         name,
		updFailed:        "Couldn't check for updates. Please try again later.",
		updLatestFmt:     "You're on the latest version (v%s).",
		updAvailableFmt:  name + " %s is available.",
		updOpen:          "Open Download Page",
		updInstall:       "Update Now",
		updInplaceFailed: "Automatic update failed — opening the download page.",
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
		trayPet:            "显示桌宠",
		trayPetHide:        "隐藏桌宠",

		takeoverTitle:  name,
		takeoverMsgFmt: "已有一个" + name + "后端在后台运行（pid %d）。\n\n停止它，并让" + name + "作为本机的后端中枢？",
		takeoverOK:     "停止并继续",
		takeoverCancel: "退出",

		quitTitle:  "退出" + short,
		quitMsg:    "退出会停止本机的" + name + "后端，已连接的编辑器、浏览器和 IM channel 都会断开。\n\n仍要退出？",
		quitOK:     "退出",
		quitCancel: "取消",

		errTitle: name,
		// 需求20260906 §5.1.2 第 6 条 的规定文案，逐字照用（`%s` 是端口，不是地址 ——
		// 规定句写的是「端口 8088」，端口号的 owner 是 `hubPort`，见 main.go）。
		// 句中的 "Octo" 指**上游产品**，是硬规则 2 的两个显式例外之一：这条提示描述的
		// 处境就是"用户装了上游 Octo"，把品牌插值进去会让他去退错的那个程序。
		// brand-exception: 第 6 条规定文案点名上游产品
		errBindFmt:  "端口 %s 已被占用。若本机正在运行 Octo 或其它程序，请先退出后再打开" + name + "。\n\n%v",
		errStopFmt:  "无法停止正在运行的后端：%v",
		errStartFmt: "无法启动后端：%v",
		// V-82：点明原因并给出两个数字 —— 只说"空间不足"等于什么都没给。
		errNoSpaceFmt: "空间不足，无法运行。数据目录所在磁盘剩余 %s，需要 %s。\n\n请清理出空间后重新打开" + name + "。",

		// V-83：规定句（§5.1.2 第 5 条），产品名插值 —— 规定句里写的是「布丁盒子」，
		// 但那是品牌值、不是可以抄进代码的字面量（硬规则 2）。
		otherCopyTitle: name,
		otherCopyMsg:   name + "已在运行，请先退出再打开另一份。",
		dialogOKText:   "好",

		updTitle:         name,
		updFailed:        "检查更新失败,请稍后重试。",
		updLatestFmt:     "已是最新版本(v%s)。",
		updAvailableFmt:  name + " %s 已发布。",
		updOpen:          "打开下载页",
		updInstall:       "立即更新",
		updInplaceFailed: "自动更新失败,已打开下载页。",
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

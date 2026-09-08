//go:build windows

package main

import "golang.org/x/sys/windows/registry"

// webview2EvergreenGUID is the WebView2 Evergreen Runtime's app GUID in the
// Edge Update registry tree. Its presence with a populated `pv` value means
// the runtime is installed. See Microsoft's WebView2 distribution docs; the
// GUID is stable across Evergreen releases.
const webview2EvergreenGUID = "{F3017226-FE2A-4295-8BDF-00C3A9A7E4C5}"

// webview2Available reports whether the WebView2 Evergreen Runtime is
// installed on this machine. The check reads the registry rather than probing
// the loader: CreateWebView2EnvironmentWithOptions pops the runtime's own
// install UI on a missing machine, which is exactly what the boot chain must
// not do before showing its readable error.
//
// A registry read failure is treated as "available", not "missing" — see
// P2 §9: a false "missing" verdict would block a machine that actually has
// the runtime, so the check errs toward letting the window try to open. The
// unreadable/missing cases are indistinguishable to a plain read, and the
// failure mode of a missing runtime (window fails to open) is caught by the
// normal startup path rather than a wrong gate here.
func webview2Available() bool {
	// The machine-wide key is installed under the 32-bit view (WOW6432Node) on
	// 64-bit Windows; the per-user key lives under the user hive without a
	// view redirect.
	return webview2ClientKey(registry.LOCAL_MACHINE, true) || webview2ClientKey(registry.CURRENT_USER, false)
}

func webview2ClientKey(root registry.Key, wow32 bool) bool {
	path := `SOFTWARE\Microsoft\EdgeUpdate\Clients\` + webview2EvergreenGUID
	access := uint32(registry.QUERY_VALUE)
	if wow32 {
		access |= registry.WOW64_32KEY
	}
	k, err := registry.OpenKey(root, path, access)
	if err != nil {
		return false
	}
	defer k.Close()
	pv, _, err := k.GetStringValue("pv")
	return err == nil && pv != ""
}

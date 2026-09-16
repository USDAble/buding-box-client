package main

import (
	"path/filepath"

	"github.com/wailsapp/wails/v3/pkg/application"

	"github.com/open-octo/octo-agent/internal/datapath"
)

// secondInstanceLaunchKey is the AdditionalData key with which a launching copy
// publishes the program directory it is running from.
//
// SecondInstanceData carries only Args, WorkingDir and AdditionalData — there is
// no executable path — so the running instance cannot otherwise learn which copy
// was double-clicked. WorkingDir is not a substitute: it is the second process's
// working directory, which a shortcut, a shell, or the OS may set to anything.
// AdditionalData exists for precisely this ("allows passing custom data from
// second instance to first"), and it is built from the *launching* process's own
// options, so each copy describes itself.
const secondInstanceLaunchKey = "programDir"

// secondInstanceAction is what a second launch means to the running instance.
type secondInstanceAction int

const (
	// activateWindow: a repeat launch of the same copy. Bringing the window
	// forward is what a user double-clicking the program they already run
	// expects.
	activateWindow secondInstanceAction = iota
	// warnOtherCopy: a different program directory. Silence here is the defect —
	// the window surfaces exactly as if their own copy had opened, so the user
	// believes the second one is running (需求20260906 §5.1.2 第 5 条).
	warnOtherCopy
)

// secondInstanceActionFor decides what to do about a second launch, given the
// program directory the launching process published and our own.
//
// An empty value on either side means "unknown": a copy that could not resolve
// its own directory publishes nothing, and the key is absent when the launching
// process predates this change. Unknown resolves to activateWindow on purpose —
// warning a user that their own copy is a second one is a worse failure than
// staying silent, so the requirement's harm is only worth acting on when a
// genuinely different directory is confirmed. This is why the degradation is
// one-sided: it can miss a warning, it can never invent one.
func secondInstanceActionFor(published, ours string) secondInstanceAction {
	if published == "" || ours == "" {
		return activateWindow
	}
	if sameProgramDir(published, ours) {
		return activateWindow
	}
	return warnOtherCopy
}

// sameProgramDir compares two program directories tolerantly.
//
// Both values come from resolving a process's own executable, so the spellings
// usually agree already. The symlink pass covers the rest: macOS reaches one
// directory as both /var/… and /private/var/…, and a portable folder can be
// opened through a link. An unresolvable path falls back to the textual answer
// instead of guessing, which keeps the unknown case (above) from turning into a
// false warning.
func sameProgramDir(a, b string) bool {
	if filepath.Clean(a) == filepath.Clean(b) {
		return true
	}
	resolvedA, errA := filepath.EvalSymlinks(a)
	resolvedB, errB := filepath.EvalSymlinks(b)
	if errA != nil || errB != nil {
		return false
	}
	return filepath.Clean(resolvedA) == filepath.Clean(resolvedB)
}

// publishProgramDir returns the AdditionalData this copy publishes about itself.
//
// A nil map is a valid "no data": the callback then cannot tell which copy
// launched and falls back to activating the window (see secondInstanceActionFor).
// Failing to resolve our own directory must not be reported as a different one.
func publishProgramDir() map[string]string {
	dir, err := datapath.ProgramDir()
	if err != nil {
		return nil
	}
	return map[string]string{secondInstanceLaunchKey: dir}
}

// secondInstanceActionFrom reads the launching copy's directory out of the data
// Wails hands the running instance and classifies the launch.
//
// Split out of the callback so the key lookup is reachable from a test: the key
// is the one part of this exchange that both processes must agree on, and it is
// invisible at the call site.
func secondInstanceActionFrom(data application.SecondInstanceData, ours string) secondInstanceAction {
	return secondInstanceActionFor(data.AdditionalData[secondInstanceLaunchKey], ours)
}

// warnOtherCopy tells the user why the copy they just opened did not start
// (需求20260906 §5.1.2 第 5 条), using the prescribed sentence.
//
// The message is the whole outcome: the running window is deliberately *not*
// raised as well. Raising it reproduces the symptom the rule is about — a window
// appears, so the double-click looks like it worked — and leaves the user unsure
// which of their two folders they are now looking at. The sentence says what to
// do, and this dialog is modal, so it cannot be missed.
func (b *nativeBridge) warnOtherCopy() {
	dlg := b.app.Dialog.Info().SetTitle(L().otherCopyTitle).SetMessage(L().otherCopyMsg)
	dlg.AddButton(L().dialogOKText).SetAsDefault()
	dlg.Show()
}

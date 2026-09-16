package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/open-octo/octo-agent/internal/brand"
	"github.com/wailsapp/wails/v3/pkg/application"
)

// TestSecondInstanceDecisionSeparatesARepeatLaunchFromAnotherCopy is the whole
// point of V-83: the two cases look identical to the running instance unless
// something distinguishes them, and only the second one must be announced.
func TestSecondInstanceDecisionSeparatesARepeatLaunchFromAnotherCopy(t *testing.T) {
	cases := []struct {
		name      string
		published string
		ours      string
		want      secondInstanceAction
	}{
		{
			name:      "same directory, same spelling",
			published: "/opt/PuddingBox",
			ours:      "/opt/PuddingBox",
			want:      activateWindow,
		},
		{
			name:      "same directory, a trailing separator",
			published: "/opt/PuddingBox/",
			ours:      "/opt/PuddingBox",
			want:      activateWindow,
		},
		{
			name:      "same directory, not normalised",
			published: "/opt/./PuddingBox",
			ours:      "/opt/PuddingBox",
			want:      activateWindow,
		},
		{
			name:      "a genuinely different folder",
			published: "/media/usb-a/PuddingBox",
			ours:      "/Users/me/PuddingBox",
			want:      warnOtherCopy,
		},
		{
			name:      "a nested copy is still a different folder",
			published: "/opt/PuddingBox/old",
			ours:      "/opt/PuddingBox",
			want:      warnOtherCopy,
		},
		{
			// The launching process's directory could not be resolved, or it is an
			// older build that publishes nothing.
			name:      "publisher unknown",
			published: "",
			ours:      "/opt/PuddingBox",
			want:      activateWindow,
		},
		{
			// We could not resolve our own directory, so there is nothing to
			// compare against.
			name:      "our own directory unknown",
			published: "/media/usb-a/PuddingBox",
			ours:      "",
			want:      activateWindow,
		},
		{
			name:      "both unknown",
			published: "",
			ours:      "",
			want:      activateWindow,
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := secondInstanceActionFor(c.published, c.ours); got != c.want {
				t.Errorf("secondInstanceActionFor(%q, %q) = %v, want %v",
					c.published, c.ours, got, c.want)
			}
		})
	}
}

// TestSameProgramDirResolvesSymlinks pins the case the textual comparison cannot
// see. On macOS the same directory is reachable as /var/… and /private/var/…, so
// a pure Clean comparison would report a second copy and show the warning for a
// plain repeat launch — the false positive the tolerant comparison exists to
// prevent.
func TestSameProgramDirResolvesSymlinks(t *testing.T) {
	real := t.TempDir()
	link := filepath.Join(t.TempDir(), "shortcut")
	if err := os.Symlink(real, link); err != nil {
		t.Skipf("cannot create a symlink here: %v", err)
	}

	if !sameProgramDir(real, link) {
		t.Errorf("sameProgramDir(%q, %q) = false; a link to the same directory is the same copy",
			real, link)
	}
	// A link to a *different* directory must still be reported as different, or
	// the tolerance would swallow the case the rule is about.
	other := t.TempDir()
	if sameProgramDir(other, link) {
		t.Errorf("sameProgramDir(%q, %q) = true; these are different directories", other, link)
	}
	// An unresolvable path must not be treated as equal to a real one.
	if sameProgramDir(filepath.Join(real, "gone"), real) {
		t.Error("sameProgramDir() matched a missing path with an existing one")
	}
}

// TestWarnOtherCopyCopyIsThePrescribedSentence pins the wording 需求20260906
// §5.1.2 第 5 条 quotes. The Chinese table is where the requirement's sentence
// applies verbatim, apart from the product name — which is interpolated rather
// than typed, because 硬规则 2 applies to a prescribed sentence too (brand-guard
// leg 1 caught exactly that here). So the assertion is the sentence with the
// brand substituted in: the tail is pinned word for word, and the head is
// asserted to be the configured name.
func TestWarnOtherCopyCopyIsThePrescribedSentence(t *testing.T) {
	const prescribedTail = "已在运行，请先退出再打开另一份。"
	if want := brand.Load().Name("zh-CN") + prescribedTail; zhStrings.otherCopyMsg != want {
		t.Errorf("zh.otherCopyMsg = %q, want the prescribed %q", zhStrings.otherCopyMsg, want)
	}

	for setName, set := range map[string]uiStrings{"en": enStrings, "zh": zhStrings} {
		// It must name the product: the user may have two folders on screen and
		// needs to know which one this is about.
		if !strings.Contains(set.otherCopyMsg, brand.Load().Name(localeFor(setName))) {
			t.Errorf("%s.otherCopyMsg does not name the product: %q", setName, set.otherCopyMsg)
		}
		// And it must carry the action, which is the half the old behaviour was
		// missing entirely. Case-insensitive because the English sentence opens
		// with the verb.
		lower := strings.ToLower(set.otherCopyMsg)
		if !strings.Contains(lower, "quit") && !strings.Contains(lower, "退出") {
			t.Errorf("%s.otherCopyMsg gives no action: %q", setName, set.otherCopyMsg)
		}
		// A title is required by the OS dialog, and a message that is the only
		// content must not be empty in either table.
		if set.otherCopyTitle == "" {
			t.Errorf("%s.otherCopyTitle is empty; the dialog has no caption", setName)
		}
	}
}

// TestPublishProgramDirNamesTheLaunchingCopy records why AdditionalData is the
// channel: SecondInstanceData has no executable path, so this key is the only
// way the running instance learns which folder was opened. If it ever publishes
// nothing on a healthy install, the warning silently stops working — the failure
// would be invisible, because the degradation is deliberately one-sided.
//
// The key is written as a literal here rather than through the constant. The two
// processes exchange it as a string, so a renamed constant that keeps compiling
// would break the exchange invisibly — both sides would then read "unknown" and
// silently stop warning.
func TestPublishProgramDirNamesTheLaunchingCopy(t *testing.T) {
	data := publishProgramDir()
	if data == nil {
		t.Fatal("publishProgramDir() = nil on a healthy install; a second launch could never be classified")
	}
	dir, ok := data["programDir"]
	if !ok {
		t.Fatalf("publishProgramDir() = %v, want the literal %q key", data, "programDir")
	}
	if dir == "" {
		t.Fatal("publishProgramDir() published an empty directory; it reads as unknown")
	}
	if !filepath.IsAbs(dir) {
		t.Errorf("publishProgramDir() = %q, want an absolute path to compare against", dir)
	}
	// And what it publishes must classify as *this* copy, or every launch of the
	// same folder would be announced as a second one.
	if got := secondInstanceActionFor(dir, dir); got != activateWindow {
		t.Errorf("classifying the published directory against itself = %v, want activateWindow", got)
	}
}

// TestSecondInstanceActionFromReadsTheWireKey covers the seam between the two
// processes: the running instance sees only what the launching copy put in
// AdditionalData, so this is where a key mismatch would show up. The literal is
// deliberate — see the note above.
func TestSecondInstanceActionFromReadsTheWireKey(t *testing.T) {
	ours := "/opt/PuddingBox"

	other := application.SecondInstanceData{
		AdditionalData: map[string]string{"programDir": "/media/usb-a/PuddingBox"},
	}
	if got := secondInstanceActionFrom(other, ours); got != warnOtherCopy {
		t.Errorf("a launch from another folder = %v, want warnOtherCopy", got)
	}

	same := application.SecondInstanceData{
		AdditionalData: map[string]string{"programDir": ours},
	}
	if got := secondInstanceActionFrom(same, ours); got != activateWindow {
		t.Errorf("a launch from our own folder = %v, want activateWindow", got)
	}

	// An older build of the launching copy publishes nothing at all. Wails
	// decodes the field as absent, which must not be mistaken for a different
	// folder.
	if got := secondInstanceActionFrom(application.SecondInstanceData{}, ours); got != activateWindow {
		t.Errorf("a launch with no published data = %v, want activateWindow", got)
	}

	// A key we do not recognise is the same case as no key: unknown, not other.
	unrecognised := application.SecondInstanceData{
		AdditionalData: map[string]string{"someOtherAppKey": "/media/usb-a/PuddingBox"},
	}
	if got := secondInstanceActionFrom(unrecognised, ours); got != activateWindow {
		t.Errorf("a launch with an unrelated key = %v, want activateWindow", got)
	}
}

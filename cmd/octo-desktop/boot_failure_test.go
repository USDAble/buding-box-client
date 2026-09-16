package main

import (
	"strings"
	"testing"

	"github.com/open-octo/octo-agent/internal/brand"
	"github.com/open-octo/octo-agent/internal/logfile"
)

// TestHumanBytesStaysReadableAtEveryUnit pins the shape of a number the user
// reads. It is copy, not logging: a figure like "4194304 B" in a startup dialog
// asks the user to do arithmetic before they can act, and "4.0 MiB" tells them
// at a glance whether the problem is one file or one disk.
func TestHumanBytesStaysReadableAtEveryUnit(t *testing.T) {
	cases := []struct {
		in   uint64
		want string
	}{
		{0, "0 B"},
		{512, "512 B"},
		{1023, "1023 B"},
		{1024, "1.0 KiB"},
		{10 << 20, "10.0 MiB"},
		{40 << 20, "40.0 MiB"},
		{3 << 30, "3.0 GiB"},
		{1 << 40, "1.0 TiB"},
	}
	for _, c := range cases {
		if got := humanBytes(c.in); got != c.want {
			t.Errorf("humanBytes(%d) = %q, want %q", c.in, got, c.want)
		}
	}
}

// TestMinFreeBytesIsTheLogEnvelope records where the floor comes from. The
// number itself is a policy choice, but it is not an arbitrary one — it is
// derived from the only figure in the tree that already says how much room this
// program needs to keep running, so the derivation is what to pin. If someone
// replaces the derivation with a typed constant, this fails and makes them say
// so; if the log envelope changes, this follows it rather than going stale.
func TestMinFreeBytesIsTheLogEnvelope(t *testing.T) {
	want := uint64(logfile.DefaultMaxBytes * (logfile.DefaultBackups + 1))
	if minFreeBytes != want {
		t.Errorf("minFreeBytes = %d, want the log envelope %d", minFreeBytes, want)
	}
	if minFreeBytes == 0 {
		t.Fatal("minFreeBytes = 0; the boot pre-check could never fire")
	}
}

// TestNoSpaceCopyCarriesBothFigures pins the copy against the reason V-82
// exists. The requirement asks the user to be told the reason for a failure
// (需求20260906 §5.1.2 第 4 条), and "not enough space" without the numbers
// leaves them guessing whether to delete a file or buy a disk.
func TestNoSpaceCopyCarriesBothFigures(t *testing.T) {
	for setName, set := range map[string]uiStrings{"en": enStrings, "zh": zhStrings} {
		values := strings.Count(set.errNoSpaceFmt, "%s")
		if values != 2 {
			t.Errorf("%s.errNoSpaceFmt has %d figures, want 2 (free and required): %q",
				setName, values, set.errNoSpaceFmt)
		}
		// And it must name the product, so the user knows what to reopen.
		if !strings.Contains(set.errNoSpaceFmt, brand.Load().Name(localeFor(setName))) {
			t.Errorf("%s.errNoSpaceFmt does not name the product: %q", setName, set.errNoSpaceFmt)
		}
	}
}

// TestPortConflictCopyNamesTheUpstreamProductAndTheAction pins 需求20260906
// §5.1.2 第 6 条's two requirements of this sentence, which are exactly what the
// old copy was missing (V-84):
//
//   - 归因 — it names Octo, the program the user most likely has running
//     (本期不与本机 Octo 双开, §9). "another program may be using it" told them
//     the port was busy but not who to quit.
//   - 动作 — it tells them what to do about it.
//
// Asserted on those properties rather than on the sentence verbatim: the wording
// may be polished, but a version that drops the attribution or the instruction
// is the defect again, and that is what this exists to catch.
func TestPortConflictCopyNamesTheUpstreamProductAndTheAction(t *testing.T) {
	cases := []struct {
		set       string
		value     string
		upstream  string
		action    string
		portFirst string
	}{
		{"en", enStrings.errBindFmt, "Octo", "open", "%s"},
		{"zh", zhStrings.errBindFmt, "Octo", "退出", "%s"},
	}
	for _, c := range cases {
		if !strings.Contains(c.value, c.upstream) {
			t.Errorf("%sen.errBindFmt does not name the upstream product %q: %q", c.set, c.upstream, c.value)
		}
		if !strings.Contains(c.value, c.action) {
			t.Errorf("%s.errBindFmt gives no action (%q): %q", c.set, c.action, c.value)
		}
		// The port is interpolated, not typed: one owner for the number
		// (hubPort), and it is the port the requirement's sentence names, not
		// the listen address.
		if !strings.HasPrefix(c.value, "端口 %s") && !strings.HasPrefix(c.value, "Port %s") {
			t.Errorf("%s.errBindFmt does not lead with the port verb: %q", c.set, c.value)
		}
		if strings.Contains(c.value, "127.0.0.1") {
			t.Errorf("%s.errBindFmt hardcodes the address instead of taking the port: %q", c.set, c.value)
		}
	}
}

// localeFor maps a table name to the brand locale it was built for.
func localeFor(set string) string {
	if set == "zh" {
		return "zh-CN"
	}
	return "en-US"
}

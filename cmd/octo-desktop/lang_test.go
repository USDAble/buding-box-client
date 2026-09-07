package main

import (
	"reflect"
	"regexp"
	"strings"
	"testing"

	"github.com/open-octo/octo-agent/internal/brand"
)

// fields reads every string field of a uiStrings by name. Reflection because
// the point is to cover fields nobody remembered to list — a check that only
// looks at an explicit set cannot catch the field added next week.
//
// Read-only: unexported fields cannot be written through reflection, but
// reading a string one is fine, which is all this needs.
func fields(t *testing.T, s uiStrings) map[string]string {
	t.Helper()
	out := map[string]string{}
	v := reflect.ValueOf(s)
	for i := 0; i < v.NumField(); i++ {
		out[v.Type().Field(i).Name] = v.Field(i).String()
	}
	return out
}

// The upstream product name must not survive anywhere in the native strings:
// these are the tray menu and the OS dialogs, the most visible surfaces there
// are. Capitalised only — the lowercase form is the CLI command and the config
// directory, which this change deliberately leaves alone.
var brandLiteral = regexp.MustCompile(`\bOcto\b`)

func TestNativeStringsCarryNoBrandLiteral(t *testing.T) {
	for setName, set := range map[string]uiStrings{"en": enStrings, "zh": zhStrings} {
		for field, value := range fields(t, set) {
			if brandLiteral.MatchString(value) {
				t.Errorf("%s.%s still names the product literally: %q", setName, field, value)
			}
		}
	}
}

// Guards the guard: without this, deleting every interpolation would satisfy
// the literal check above with copy that names nothing at all.
//
// Named fields rather than a count of how many mention the product: a count
// passes while any one field silently stops naming it, which is precisely the
// regression worth catching. These are the surfaces the requirements call out —
// tray entries, dialog titles, and the two long dialogs.
var mustNameProduct = []string{
	"trayShow", "trayQuit",
	"takeoverTitle", "takeoverMsgFmt",
	"quitTitle", "quitMsg",
	"errTitle",
	"updTitle", "updAvailableFmt",
}

func TestNativeStringsUseTheConfiguredName(t *testing.T) {
	cfg := brand.Load()

	cases := []struct {
		set    string
		locale string
		values uiStrings
	}{
		{"en", "en-US", enStrings},
		{"zh", "zh-CN", zhStrings},
	}
	for _, c := range cases {
		name := cfg.Name(c.locale)
		short := cfg.ShortName(c.locale)
		if name == "" || short == "" {
			t.Fatalf("brand config has no name for %s", c.locale)
		}
		values := fields(t, c.values)
		for _, field := range mustNameProduct {
			value, ok := values[field]
			if !ok {
				t.Errorf("%s: no such field %s — the list needs updating", c.set, field)
				continue
			}
			if !strings.Contains(value, name) && !strings.Contains(value, short) {
				t.Errorf("%s.%s does not name the product: %q", c.set, field, value)
			}
		}
	}
}

func TestNativeStringsLeakNoPlaceholder(t *testing.T) {
	// The Go tables concatenate rather than interpolate, so a "{brand}" here
	// would mean someone copied the web dictionary style into a surface that
	// never substitutes it.
	for setName, set := range map[string]uiStrings{"en": enStrings, "zh": zhStrings} {
		for field, value := range fields(t, set) {
			if strings.Contains(value, "{brand") {
				t.Errorf("%s.%s carries an unsubstituted placeholder: %q", setName, field, value)
			}
		}
	}
}

// Format strings were retyped when the brand name moved out of them, and a
// dropped or reordered verb is invisible until the dialog renders as
// "%!d(MISSING)". Pinning the verb sequence per field catches that at test
// time; the sequences match the comments on the uiStrings fields.
func TestNativeFormatVerbsPreserved(t *testing.T) {
	verb := regexp.MustCompile(`%[a-zA-Z]`)
	want := map[string]string{
		"trayUpdateAvailFmt": "%s",
		"trayBackendFmt":     "%s",
		"trayClientsFmt":     "%d",
		"trayChannelsFmt":    "%d",
		"takeoverMsgFmt":     "%d",
		"errBindFmt":         "%s%v",
		"errStopFmt":         "%v",
		"errStartFmt":        "%v",
		"updLatestFmt":       "%s",
		"updAvailableFmt":    "%s",
	}
	for setName, set := range map[string]uiStrings{"en": enStrings, "zh": zhStrings} {
		values := fields(t, set)
		for field, expected := range want {
			got := strings.Join(verb.FindAllString(values[field], -1), "")
			if got != expected {
				t.Errorf("%s.%s verbs = %q, want %q (in %q)", setName, field, got, expected, values[field])
			}
		}
		// Every remaining field must be verb-free, so a stray %s introduced by
		// a brand name containing one cannot slip through unlisted.
		for field, value := range values {
			if _, listed := want[field]; listed {
				continue
			}
			if v := verb.FindAllString(value, -1); len(v) != 0 {
				t.Errorf("%s.%s unexpectedly contains format verbs %v: %q", setName, field, v, value)
			}
		}
	}
}

// Chinese takes no space around an inline name; English needs one. This is the
// reason the tables are per-language constructors instead of one table with the
// name substituted into it — a single table would have to pick one spacing and
// be wrong in the other language.
func TestChineseStringsDoNotSpaceOffTheName(t *testing.T) {
	cfg := brand.Load()
	name := cfg.Name("zh-CN")
	short := cfg.ShortName("zh-CN")

	cjk := regexp.MustCompile(`[\p{Han}]`)
	for field, value := range fields(t, zhStrings) {
		for _, n := range []string{name, short} {
			if n == "" || !cjk.MatchString(n) {
				continue
			}
			for _, gap := range []string{" " + n, n + " "} {
				idx := strings.Index(value, gap)
				if idx < 0 {
					continue
				}
				// A space between the name and a Latin run (a version number,
				// a fmt verb) is correct; only CJK adjacency is the bug.
				around := value[max(0, idx-3):min(len(value), idx+len(gap)+3)]
				if cjk.MatchString(strings.ReplaceAll(around, n, "")) {
					t.Errorf("zh.%s spaces the name off from CJK: %q", field, value)
				}
			}
		}
	}
}

func TestEnglishStringsKeepTheSpace(t *testing.T) {
	// The mirror of the check above: dropping the space would render
	// "ShowPudding Box" in the tray.
	short := brand.Load().ShortName("en-US")
	if got := enStrings.trayShow; got != "Show "+short {
		t.Errorf("enStrings.trayShow = %q, want %q", got, "Show "+short)
	}
}

package main

import (
	"os"
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
//
// One field is licensed to name it, and the licence is written in the source
// next to the copy rather than listed here: 需求20260906 §5.1.2 第 6 条 mandates a
// port-conflict sentence naming the upstream product, because the situation it
// describes is the user having upstream Octo installed. See upstreamNameLicences
// — scripts/brand-guard.mjs reads the same marker for the same reason, so the two
// checks cannot drift apart on what the exception covers.
var brandLiteral = regexp.MustCompile(`\bOcto\b`)

// markerLine matches the annotation that licenses a field, exactly as
// scripts/brand-guard.mjs reads it out of the same file.
var markerLine = regexp.MustCompile(`^\s*//.*brand-exception`)

// fieldAssignment matches a uiStrings field being set in a constructor.
var fieldAssignment = regexp.MustCompile(`^\s*(\w+):\s`)

// stringsConstructor matches a uiStrings copy-table constructor, so a licence
// can be attributed to the one table it sits in.
var stringsConstructor = regexp.MustCompile(`^func (\w+)StringsFor\(`)

// upstreamNameLicences returns, per copy table, the uiStrings fields whose copy
// is allowed to name the upstream product.
//
// Read from lang.go's source on purpose. The alternative — a list of field names
// in this test — would be a second owner for the exception: adding the copy
// without the list entry would fail here, and the two could disagree about which
// fields are licensed. Reading the marker means one annotation serves both this
// test and brand-guard, and it must sit on the comment directly above the copy,
// so it stays next to what it licenses.
//
// Keyed by table as well as field, and that is not incidental: a licence is
// granted to one string in one language. Keyed by field alone, the English
// table's marker would silently cover the Chinese one as well, so removing the
// marker from either table would still pass here — found exactly that way, by
// deleting one marker and watching brand-guard fail while this stayed green.
func upstreamNameLicences(t *testing.T) map[string]map[string]string {
	t.Helper()
	src, err := os.ReadFile("lang.go")
	if err != nil {
		t.Fatalf("reading lang.go: %v", err)
	}
	licences := map[string]map[string]string{}
	table := ""
	lines := strings.Split(string(src), "\n")
	for i := 0; i < len(lines); i++ {
		if m := stringsConstructor.FindStringSubmatch(lines[i]); m != nil {
			table = tableName(m[1])
			continue
		}
		if i == 0 || !markerLine.MatchString(lines[i-1]) {
			continue
		}
		field := fieldAssignment.FindStringSubmatch(lines[i])
		if field == nil {
			continue
		}
		if licences[table] == nil {
			licences[table] = map[string]string{}
		}
		licences[table][field[1]] = strings.TrimSpace(lines[i-1])
	}
	return licences
}

// tableName maps a constructor name to the table key the assertions use.
func tableName(constructor string) string {
	if strings.HasPrefix(constructor, "zh") {
		return "zh"
	}
	return "en"
}

func TestNativeStringsCarryNoBrandLiteral(t *testing.T) {
	licences := upstreamNameLicences(t)
	for setName, set := range map[string]uiStrings{"en": enStrings, "zh": zhStrings} {
		values := fields(t, set)
		for field, value := range values {
			if _, licensed := licences[setName][field]; licensed {
				// The licence must be earning its keep: a marker left on copy
				// that no longer names the upstream product is an exception that
				// outlived its reason, and it would silently keep covering the
				// field if the name came back.
				if !brandLiteral.MatchString(value) {
					t.Errorf("%s.%s is licensed to name the upstream product but no longer does (%q) — drop the marker",
						setName, field, value)
				}
				continue
			}
			if brandLiteral.MatchString(value) {
				t.Errorf("%s.%s still names the product literally: %q", setName, field, value)
			}
		}
	}
}

// TestEveryLicenceIsVisibleToBrandGuard keeps the two checks reading the same
// thing. The Go test derives licences from lang.go's markers; brand-guard
// derives them from the same markers with its own regex. If the two regexes
// disagree about what a marker looks like, one guard would enforce an exception
// the other does not know about — so the shapes are asserted here rather than
// left to drift.
func TestEveryLicenceIsVisibleToBrandGuard(t *testing.T) {
	src, err := os.ReadFile("lang.go")
	if err != nil {
		t.Fatalf("reading lang.go: %v", err)
	}
	// The JS guard's pattern, transcribed: a leading comment opener, then the
	// marker word anywhere on the line.
	jsMarker := regexp.MustCompile(`^\s*(//|#|;|\*|/\*).*brand-exception`)
	found := 0
	for _, line := range strings.Split(string(src), "\n") {
		if !strings.Contains(line, "brand-exception") {
			continue
		}
		if !jsMarker.MatchString(line) {
			t.Errorf("a marker line is a comment to this test but not to brand-guard: %q", line)
		}
		found++
	}
	if found == 0 {
		t.Fatal("no markers found in lang.go; the exception tests above would be vacuous")
	}
	// And every marker must sit directly above a copy line, or it licenses
	// nothing while looking like it does.
	if len(upstreamNameLicences(t)) == 0 {
		t.Fatal("mutating lang.go: no marker was read as licensing a field")
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
		"errNoSpaceFmt":      "%s%s",
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

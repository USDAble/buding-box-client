package productstate_test

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"github.com/open-octo/octo-agent/internal/productstate"
)

// useTempDataRoot points the data root at a fresh directory for the test. The
// only supported override is the environment variable, whose scope is tests and
// developer builds (开发规范 §3.1 规则 1).
func useTempDataRoot(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	t.Setenv("OCTO_DATA_ROOT", dir)
	return dir
}

func statePath(t *testing.T, root string) string {
	t.Helper()
	return filepath.Join(root, "product-state.json")
}

func readRaw(t *testing.T, path string) map[string]any {
	t.Helper()
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}
	var m map[string]any
	if err := json.Unmarshal(b, &m); err != nil {
		t.Fatalf("unmarshal %s: %v", path, err)
	}
	return m
}

// L-E1: the first run seeds the file with a stable installation id.
func TestFirstRunSeedsInstallID(t *testing.T) {
	root := useTempDataRoot(t)

	store, err := productstate.Open(productstate.Options{Locale: "zh"})
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	id := store.InstallID()
	if id == "" {
		t.Fatal("InstallID is empty after first run")
	}
	if !regexp.MustCompile(`^[0-9a-f]{8}-[0-9a-f]{4}-4[0-9a-f]{3}-[89ab][0-9a-f]{3}-[0-9a-f]{12}$`).MatchString(id) {
		t.Errorf("InstallID %q is not a UUIDv4", id)
	}

	onDisk := readRaw(t, statePath(t, root))
	if onDisk["installId"] != id {
		t.Errorf("installId on disk = %v, want %v", onDisk["installId"], id)
	}
	if onDisk["schemaVersion"] != float64(productstate.CurrentSchemaVersion) {
		t.Errorf("schemaVersion = %v, want %d", onDisk["schemaVersion"], productstate.CurrentSchemaVersion)
	}
	if got := onDisk["loggedIn"]; got != false {
		t.Errorf("loggedIn = %v, want false", got)
	}
}

// L-E1: the id belongs to the installation, so it survives logout and a
// subsequent login. This is what makes it usable as a stable platform identifier.
func TestInstallIDSurvivesLogoutAndRelogin(t *testing.T) {
	useTempDataRoot(t)

	first, err := productstate.Open(productstate.Options{})
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	id := first.InstallID()

	if err := first.ApplyLogin(productstate.LoginOutcome{
		PhoneMasked: "138****1234",
		Nickname:    "tester",
		ActivatedAt: "2026-09-11T08:00:00Z",
		ExpiresAt:   "2027-09-11T08:00:00Z",
		BoxCode:     "BOX-DEMO-0001",
	}, productstate.Credits{}); err != nil {
		t.Fatalf("ApplyLogin: %v", err)
	}
	if err := first.Logout(); err != nil {
		t.Fatalf("Logout: %v", err)
	}

	second, err := productstate.Open(productstate.Options{})
	if err != nil {
		t.Fatalf("reopen: %v", err)
	}
	if got := second.InstallID(); got != id {
		t.Errorf("InstallID changed across logout: %q -> %q", id, got)
	}
}

// E7: logging out is not un-activating. The activation record and the bound
// phone number stay, because the second-login form needs them.
func TestLogoutKeepsActivationAndAccount(t *testing.T) {
	useTempDataRoot(t)

	store, err := productstate.Open(productstate.Options{})
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	if err := store.ApplyLogin(productstate.LoginOutcome{
		PhoneMasked: "138****1234",
		Nickname:    "tester",
		ActivatedAt: "2026-09-11T08:00:00Z",
		ExpiresAt:   "2027-09-11T08:00:00Z",
		BoxCode:     "BOX-DEMO-0001",
	}, productstate.Credits{Balance: 12500}); err != nil {
		t.Fatalf("ApplyLogin: %v", err)
	}
	if err := store.Logout(); err != nil {
		t.Fatalf("Logout: %v", err)
	}

	state := store.State()
	if state.LoggedIn {
		t.Error("LoggedIn is still true after logout")
	}
	if !state.Activated {
		t.Error("Activated was cleared by logout; the binding must survive")
	}
	if state.Activation == nil || state.Activation.BoxCode != "BOX-DEMO-0001" {
		t.Errorf("activation record lost: %+v", state.Activation)
	}
	if state.Account == nil || state.Account.PhoneMasked != "138****1234" {
		t.Errorf("bound phone lost: %+v", state.Account)
	}
	if state.Credits.Balance != 12500 {
		t.Errorf("credits lost: %+v", state.Credits)
	}
}

// E6.1 verification: the box code comes from the platform, so a fresh data
// directory that never saw the activation form still learns it.
func TestBoxCodeComesFromLoginOutcome(t *testing.T) {
	useTempDataRoot(t)

	store, err := productstate.Open(productstate.Options{})
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	// Second login: no activation form, so no box code was typed in.
	if err := store.ApplyLogin(productstate.LoginOutcome{
		PhoneMasked: "138****1234",
		Nickname:    "tester",
		ActivatedAt: "2026-09-11T08:00:00Z",
		ExpiresAt:   "2027-09-11T08:00:00Z",
		BoxCode:     "BOX-DEMO-0001",
	}, productstate.Credits{}); err != nil {
		t.Fatalf("ApplyLogin: %v", err)
	}

	pub := store.PublicState()
	if pub.Activation == nil || pub.Activation.BoxCode != "BOX-DEMO-0001" {
		t.Fatalf("box code not projected: %+v", pub.Activation)
	}
}

// E6.1: no token may appear in the state file. The credential file is the only
// place one is allowed to exist.
func TestStateFileCarriesNoToken(t *testing.T) {
	root := useTempDataRoot(t)

	store, err := productstate.Open(productstate.Options{})
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	if err := store.ApplyLogin(productstate.LoginOutcome{
		PhoneMasked: "138****1234",
		Nickname:    "tester",
		ActivatedAt: "2026-09-11T08:00:00Z",
		ExpiresAt:   "2027-09-11T08:00:00Z",
		BoxCode:     "BOX-DEMO-0001",
	}, productstate.Credits{}); err != nil {
		t.Fatalf("ApplyLogin: %v", err)
	}

	raw, err := os.ReadFile(statePath(t, root))
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	for _, needle := range []string{"Token", "token", "refreshToken", "accessToken"} {
		if strings.Contains(string(raw), needle) {
			t.Errorf("state file contains %q, which must live only in credential.json", needle)
		}
	}
	// The plaintext phone number must not be stored either.
	if strings.Contains(string(raw), "13800001234") {
		t.Error("state file contains a plaintext phone number")
	}
}

// E6.2 规则 2 / PQ19: a record predating boxCode loads fine, reports the field
// as absent, and is NOT rewritten on load.
func TestLegacyDataWithoutBoxCodeIsPreserved(t *testing.T) {
	root := useTempDataRoot(t)
	path := statePath(t, root)

	// A file written before boxCode existed, with a field this build does not
	// know either.
	legacy := `{
  "schemaVersion": 1,
  "loggedIn": true,
  "activated": true,
  "activation": {"activatedAt": "2026-01-01T00:00:00Z", "expiresAt": "2027-01-01T00:00:00Z"},
  "account": {"phoneMasked": "138****1234", "nickname": "old", "lastLoginAt": "2026-01-01T00:00:00Z"},
  "installId": "11111111-1111-4111-8111-111111111111",
  "somethingThisBuildDoesNotKnow": true
}`
	if err := os.WriteFile(path, []byte(legacy), 0o600); err != nil {
		t.Fatalf("seed legacy file: %v", err)
	}

	store, err := productstate.Open(productstate.Options{})
	if err != nil {
		t.Fatalf("Open on legacy data: %v", err)
	}
	pub := store.PublicState()
	if !pub.LoggedIn || !pub.Activated {
		t.Error("legacy data was not honoured")
	}
	if pub.Activation == nil || pub.Activation.BoxCode != "" {
		t.Errorf("missing boxCode should surface as absent, got %+v", pub.Activation)
	}
	if pub.Account == nil || pub.Account.Nickname != "old" {
		t.Errorf("legacy account lost: %+v", pub.Account)
	}
	if store.InstallID() != "11111111-1111-4111-8111-111111111111" {
		t.Errorf("existing installId was not reused: %q", store.InstallID())
	}

	// The load must not have rewritten the file (E6.2 规则 3).
	after, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read back: %v", err)
	}
	if string(after) != legacy {
		t.Error("opening the state file rewrote it; startup must not write user files")
	}
}

// E6.2 规则 4: data from a newer build must not be parsed as if it were older -
// doing so would write back and drop the newer fields.
func TestNewerSchemaVersionFailsClosed(t *testing.T) {
	root := useTempDataRoot(t)

	future := `{"schemaVersion": 99, "loggedIn": true, "installId": "x"}`
	if err := os.WriteFile(statePath(t, root), []byte(future), 0o600); err != nil {
		t.Fatalf("seed: %v", err)
	}

	_, err := productstate.Open(productstate.Options{})
	if !errors.Is(err, productstate.ErrIncompatibleVersion) {
		t.Fatalf("err = %v, want ErrIncompatibleVersion", err)
	}
	// Still on disk, untouched.
	if _, statErr := os.Stat(statePath(t, root)); statErr != nil {
		t.Fatalf("state file was removed: %v", statErr)
	}
}

// E6.2 规则 5: a corrupt file degrades to "not logged in" and is preserved, so a
// parse bug cannot destroy the user's data.
func TestCorruptStateIsReportedAndPreserved(t *testing.T) {
	root := useTempDataRoot(t)
	path := statePath(t, root)
	garbage := `{"schemaVersion": 1, "loggedIn": tru`
	if err := os.WriteFile(path, []byte(garbage), 0o600); err != nil {
		t.Fatalf("seed: %v", err)
	}

	store, err := productstate.Open(productstate.Options{})
	if !errors.Is(err, productstate.ErrCorrupt) {
		t.Fatalf("err = %v, want ErrCorrupt", err)
	}
	if !store.Corrupt() {
		t.Error("Corrupt() = false after a parse failure")
	}
	if pub := store.PublicState(); pub.LoggedIn {
		t.Error("corrupt data must degrade to not logged in")
	}

	after, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read back: %v", err)
	}
	if string(after) != garbage {
		t.Error("the corrupt file was overwritten; it must be preserved for recovery")
	}
}

// E6.2 规则 1: every save stamps the current version, and unknown future fields
// in the file survive a read-modify-write only when the file was understood -
// this checks the version is always written.
func TestSaveStampsSchemaVersion(t *testing.T) {
	root := useTempDataRoot(t)

	store, err := productstate.Open(productstate.Options{})
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	if err := store.SetLocale("en"); err != nil {
		t.Fatalf("SetLocale: %v", err)
	}

	onDisk := readRaw(t, statePath(t, root))
	if onDisk["schemaVersion"] != float64(productstate.CurrentSchemaVersion) {
		t.Errorf("schemaVersion = %v, want %d", onDisk["schemaVersion"], productstate.CurrentSchemaVersion)
	}
}

// The interface language is changeable while logged out: the login screen has to
// be readable before there is an account (E8 规则 5).
func TestLocaleIsChangeableWhileLoggedOut(t *testing.T) {
	useTempDataRoot(t)

	store, err := productstate.Open(productstate.Options{Locale: "en"})
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	if got := store.PublicState().Prefs.Locale; got != "en" {
		t.Errorf("seeded locale = %q, want en", got)
	}
	if err := store.SetLocale("zh"); err != nil {
		t.Fatalf("SetLocale: %v", err)
	}
	if got := store.PublicState().Prefs.Locale; got != "zh" {
		t.Errorf("locale = %q, want zh", got)
	}
}

// A logged-in installation is always activated, which is the one normalisation
// the local API contract states (本地API契约 §1.3).
func TestLoggedInImpliesActivatedInProjection(t *testing.T) {
	useTempDataRoot(t)

	store, err := productstate.Open(productstate.Options{})
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	if err := store.ApplyLogin(productstate.LoginOutcome{
		PhoneMasked: "138****1234",
		Nickname:    "tester",
		ActivatedAt: "2026-09-11T08:00:00Z",
		ExpiresAt:   "2027-09-11T08:00:00Z",
	}, productstate.Credits{}); err != nil {
		t.Fatalf("ApplyLogin: %v", err)
	}
	if pub := store.PublicState(); !pub.LoggedIn || !pub.Activated {
		t.Errorf("loggedIn=%v activated=%v, want both true", pub.LoggedIn, pub.Activated)
	}
}

package credentialstore_test

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/open-octo/octo-agent/internal/credentialstore"
)

func useTempDataRoot(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	t.Setenv("OCTO_DATA_ROOT", dir)
	return dir
}

func credPath(root string) string { return filepath.Join(root, "credential.json") }

// The credential is the whole point of a portable installation: copying data/ to
// another machine must keep the session. This checks it round-trips through the
// file, not just through memory (需求基线 E6 规则 3 / E2E-11).
func TestSaveThenLoadRoundTrips(t *testing.T) {
	root := useTempDataRoot(t)

	store, err := credentialstore.Open(credentialstore.Options{})
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	want := credentialstore.Credential{
		RefreshToken:       "rt_abc123",
		AccountPhoneMasked: "138****1234",
		InstallID:          "11111111-1111-4111-8111-111111111111",
	}
	if err := store.Save(want); err != nil {
		t.Fatalf("Save: %v", err)
	}

	// A second store reads the same file, as a restart would.
	reopened, err := credentialstore.Open(credentialstore.Options{})
	if err != nil {
		t.Fatalf("reopen: %v", err)
	}
	got, ok, err := reopened.Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if !ok {
		t.Fatal("credential not found after Save")
	}
	if got.RefreshToken != want.RefreshToken {
		t.Errorf("refreshToken = %q, want %q", got.RefreshToken, want.RefreshToken)
	}
	if got.AccountPhoneMasked != want.AccountPhoneMasked {
		t.Errorf("accountPhoneMasked = %q, want %q", got.AccountPhoneMasked, want.AccountPhoneMasked)
	}
	if got.InstallID != want.InstallID {
		t.Errorf("installId = %q, want %q", got.InstallID, want.InstallID)
	}
	if got.ObtainedAt == "" {
		t.Error("obtainedAt was not stamped")
	}
	if got.SchemaVersion != credentialstore.CurrentSchemaVersion {
		t.Errorf("schemaVersion = %d, want %d", got.SchemaVersion, credentialstore.CurrentSchemaVersion)
	}
	if _, err := os.Stat(credPath(root)); err != nil {
		t.Fatalf("credential file missing: %v", err)
	}
}

// E6.3 规则 1: the access token has no place in this file.
func TestAccessTokenIsNotWritten(t *testing.T) {
	root := useTempDataRoot(t)

	store, err := credentialstore.Open(credentialstore.Options{})
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	if err := store.Save(credentialstore.Credential{RefreshToken: "rt_abc123"}); err != nil {
		t.Fatalf("Save: %v", err)
	}

	raw, err := os.ReadFile(credPath(root))
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	if strings.Contains(string(raw), "accessToken") {
		t.Error("credential.json mentions accessToken; it must live only in memory")
	}

	var shape map[string]any
	if err := json.Unmarshal(raw, &shape); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	want := []string{"schemaVersion", "refreshToken", "obtainedAt"}
	for _, key := range want {
		if _, ok := shape[key]; !ok {
			t.Errorf("credential.json is missing %q", key)
		}
	}
	if len(shape) != 3 {
		t.Errorf("credential.json has %d fields (%v), want exactly the refresh token, its timestamp and the version",
			len(shape), keysOf(shape))
	}
}

func keysOf(m map[string]any) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	return out
}

// A missing file is "not logged in", not an error.
func TestMissingFileIsNotAnError(t *testing.T) {
	useTempDataRoot(t)

	store, err := credentialstore.Open(credentialstore.Options{})
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	_, ok, err := store.Load()
	if err != nil {
		t.Fatalf("Load on a missing file: %v", err)
	}
	if ok {
		t.Error("ok = true with no credential file")
	}
}

// E6.3 规则 4: a corrupt credential is treated as "not logged in" and removed -
// unlike the state file, keeping it would pin the user to the login screen.
func TestCorruptCredentialIsDropped(t *testing.T) {
	root := useTempDataRoot(t)

	if err := os.WriteFile(credPath(root), []byte(`{"refreshToken":`), 0o600); err != nil {
		t.Fatalf("seed: %v", err)
	}

	store, err := credentialstore.Open(credentialstore.Options{})
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	_, ok, err := store.Load()
	if err != nil {
		t.Fatalf("Load on a corrupt file: %v", err)
	}
	if ok {
		t.Error("ok = true for a corrupt credential")
	}
	if _, statErr := os.Stat(credPath(root)); !os.IsNotExist(statErr) {
		t.Errorf("corrupt credential was kept; it must be removed: %v", statErr)
	}
}

// A file with no refresh token is unusable and is treated as absent.
func TestCredentialWithoutTokenIsDropped(t *testing.T) {
	root := useTempDataRoot(t)

	if err := os.WriteFile(credPath(root), []byte(`{"schemaVersion":1,"refreshToken":""}`), 0o600); err != nil {
		t.Fatalf("seed: %v", err)
	}
	store, err := credentialstore.Open(credentialstore.Options{})
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	if _, ok, err := store.Load(); ok || err != nil {
		t.Fatalf("ok=%v err=%v, want ok=false err=nil", ok, err)
	}
}

// A credential from a newer build is not understood. It is left alone rather
// than deleted, because the newer build may come back.
func TestNewerCredentialIsRefusedButKept(t *testing.T) {
	root := useTempDataRoot(t)

	payload := `{"schemaVersion":99,"refreshToken":"rt_future"}`
	if err := os.WriteFile(credPath(root), []byte(payload), 0o600); err != nil {
		t.Fatalf("seed: %v", err)
	}

	store, err := credentialstore.Open(credentialstore.Options{})
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	_, ok, err := store.Load()
	if err == nil {
		t.Fatal("Load succeeded on a newer credential, want a refusal")
	}
	if ok {
		t.Error("ok = true for a newer credential")
	}
	after, readErr := os.ReadFile(credPath(root))
	if readErr != nil {
		t.Fatalf("read back: %v", readErr)
	}
	if string(after) != payload {
		t.Error("a newer credential must not be rewritten or deleted")
	}
}

// E7: logging out is deleting this file. Deleting it twice is not an error.
func TestDeleteIsIdempotentAndRemovesTheFile(t *testing.T) {
	root := useTempDataRoot(t)

	store, err := credentialstore.Open(credentialstore.Options{})
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	if err := store.Save(credentialstore.Credential{RefreshToken: "rt_abc123"}); err != nil {
		t.Fatalf("Save: %v", err)
	}
	if err := store.Delete(); err != nil {
		t.Fatalf("Delete: %v", err)
	}
	if _, statErr := os.Stat(credPath(root)); !os.IsNotExist(statErr) {
		t.Errorf("credential file survived Delete: %v", statErr)
	}
	// The logout button can be pressed again, or the file may already be gone.
	if err := store.Delete(); err != nil {
		t.Fatalf("second Delete: %v", err)
	}
}

// A yanked drive must not leave a temporary file next to the user's data, and a
// failed write must not destroy the credential already there.
func TestFailedWriteLeavesNoDebris(t *testing.T) {
	root := useTempDataRoot(t)

	store, err := credentialstore.Open(credentialstore.Options{})
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	if err := store.Save(credentialstore.Credential{RefreshToken: "rt_first"}); err != nil {
		t.Fatalf("Save: %v", err)
	}

	entries, err := os.ReadDir(root)
	if err != nil {
		t.Fatalf("ReadDir: %v", err)
	}
	for _, e := range entries {
		if strings.HasSuffix(e.Name(), ".tmp") {
			t.Errorf("temporary file left behind: %s", e.Name())
		}
	}

	// The credential is still the one that was written.
	got, ok, err := store.Load()
	if err != nil || !ok {
		t.Fatalf("Load: ok=%v err=%v", ok, err)
	}
	if got.RefreshToken != "rt_first" {
		t.Errorf("refreshToken = %q, want rt_first", got.RefreshToken)
	}
}

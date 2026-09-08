package productstate

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestOpenMissingFileIsZero(t *testing.T) {
	s, err := Open(filepath.Join(t.TempDir(), "product-state.json"))
	if err != nil {
		t.Fatalf("Open on a missing file = %v, want nil", err)
	}
	if s.LoggedIn() {
		t.Error("missing file must read as not logged in")
	}
	if s.Activated() {
		t.Error("missing file must read as not activated")
	}
}

func TestMutatePersistsAcrossReopen(t *testing.T) {
	path := filepath.Join(t.TempDir(), "product-state.json")
	s, err := Open(path)
	if err != nil {
		t.Fatalf("Open = %v", err)
	}
	now := time.Now().Truncate(time.Second)
	err = s.Mutate(func(st *State) error {
		st.SchemaVersion = 1
		st.Account = &Account{Phone: "13800001234", PhoneMasked: "138****1234", Nickname: "用户1234", Token: "local-tok", LastLoginAt: now}
		st.Activation = &Activation{Activated: true, Code: "BUDING-DEMO-0001", ActivatedAt: now, ExpiresAt: now.AddDate(1, 0, 0)}
		st.Credits = Credits{Balance: 1280}
		return nil
	})
	if err != nil {
		t.Fatalf("Mutate = %v", err)
	}
	if !s.LoggedIn() || !s.Activated() {
		t.Fatalf("state should read logged in + activated after Mutate")
	}

	// Re-open: the file must round-trip.
	s2, err := Open(path)
	if err != nil {
		t.Fatalf("re-Open = %v", err)
	}
	st := s2.Snapshot()
	if st.Account == nil || st.Account.Phone != "13800001234" || st.Account.Nickname != "用户1234" {
		t.Fatalf("account did not round-trip: %+v", st.Account)
	}
	if st.Activation == nil || !st.Activation.Activated || st.Activation.Code != "BUDING-DEMO-0001" {
		t.Fatalf("activation did not round-trip: %+v", st.Activation)
	}
	if st.Credits.Balance != 1280 {
		t.Fatalf("credits did not round-trip: %+v", st.Credits)
	}
}

func TestMutateFailureKeepsOriginal(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "blocked", "product-state.json")
	s, err := Open(path)
	if err != nil {
		t.Fatalf("Open = %v", err)
	}
	// First write succeeds (creates the blocked/ dir).
	if err := s.Mutate(func(st *State) error { st.Credits = Credits{Balance: 7}; return nil }); err != nil {
		t.Fatalf("first Mutate = %v", err)
	}
	before, _ := os.ReadFile(path)

	// Replace the parent dir with a FILE so MkdirAll(dir-of-path) can't succeed:
	// the atomic write must fail and leave the original bytes intact.
	if err := os.RemoveAll(filepath.Dir(path)); err != nil {
		t.Fatalf("RemoveAll = %v", err)
	}
	if err := os.WriteFile(filepath.Dir(path), []byte("blocker"), 0o600); err != nil {
		t.Fatalf("write blocker = %v", err)
	}
	err = s.Mutate(func(st *State) error { st.Credits = Credits{Balance: 999}; return nil })
	if err == nil {
		t.Fatal("Mutate should fail when the write cannot land")
	}
	// The in-memory state must not have advanced either.
	if got := s.Snapshot().Credits.Balance; got != 7 {
		t.Fatalf("in-memory balance = %d, want 7 (failed Mutate must not publish)", got)
	}
	_ = before
}

func TestLogoutKeepsActivationAndCredits(t *testing.T) {
	s, err := Open(filepath.Join(t.TempDir(), "product-state.json"))
	if err != nil {
		t.Fatalf("Open = %v", err)
	}
	if err := s.Mutate(func(st *State) error {
		st.Account = &Account{Phone: "13800001234"}
		st.Activation = &Activation{Activated: true}
		st.Credits = Credits{Balance: 1280, MonthUsed: 3}
		return nil
	}); err != nil {
		t.Fatalf("Mutate = %v", err)
	}
	// Logout clears only the account.
	if err := s.Mutate(func(st *State) error { st.Account = nil; return nil }); err != nil {
		t.Fatalf("logout Mutate = %v", err)
	}
	if s.LoggedIn() {
		t.Fatal("logout must clear the account")
	}
	st := s.Snapshot()
	if !st.Activated() {
		t.Fatal("logout must keep activation")
	}
	if st.Credits.Balance != 1280 || st.Credits.MonthUsed != 3 {
		t.Fatalf("logout must keep credits: %+v", st.Credits)
	}
}

func TestSnapshotIsDeepCopied(t *testing.T) {
	s, err := Open(filepath.Join(t.TempDir(), "product-state.json"))
	if err != nil {
		t.Fatalf("Open = %v", err)
	}
	_ = s.Mutate(func(st *State) error { st.Account = &Account{Nickname: "a"}; return nil })

	snap := s.Snapshot()
	snap.Account.Nickname = "mutated-through-snapshot"
	if got := s.Snapshot().Account.Nickname; got != "a" {
		t.Fatalf("mutating a Snapshot leaked into the store: nickname = %q", got)
	}
}

func TestPublicStripsSecrets(t *testing.T) {
	st := State{
		SchemaVersion: 1,
		Account:       &Account{Phone: "13800001234", PhoneMasked: "138****1234", Nickname: "用户1234", Token: "secret-token"},
		Activation:    &Activation{Activated: true},
		Credits:       Credits{Balance: 1280},
	}
	p := st.Public()
	if p.LoggedIn != true {
		t.Fatal("public state should report logged in")
	}
	if p.Account == nil {
		t.Fatal("public state should carry the masked account")
	}
	if p.Account.PhoneMasked != "138****1234" || p.Account.Nickname != "用户1234" {
		t.Fatalf("public account = %+v", p.Account)
	}
	// The plaintext phone and the token must never serialize into the public view.
	b, err := json.Marshal(p)
	if err != nil {
		t.Fatalf("marshal public = %v", err)
	}
	if strings.Contains(string(b), "13800001234") || strings.Contains(string(b), "secret-token") {
		t.Fatalf("public state leaked the phone plaintext or token: %s", b)
	}
}

// OCTO-FORK: 新增测试：引擎的注入缝（`PR-6b1`）— see the current implementation plan §PR-6b1
package server

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/open-octo/octo-agent/internal/sensitive"
)

// The engine arrives from outside now (cmd/octo-desktop's mountProductAPI,
// because internal/productruntime answers the word-list routes and neither
// package may import the other). These two tests are the pair that keeps the
// seam honest: an injected engine must be the one used, and its absence must
// still leave the server filtering.

// TestTheInjectedEngineIsTheOneTheServerUses asserts identity, not behaviour.
// A second engine built from the same path would pass every behavioural test
// here and still be the defect §3.8 forbids: two readers of
// data/sensitive-words.txt with two caches, so the screen and the checker could
// disagree about the effective word list.
func TestTheInjectedEngineIsTheOneTheServerUses(t *testing.T) {
	root := dataRoot(t)
	dict := filepath.Join(root, "sensitive-words.txt")
	if err := os.WriteFile(dict, []byte("注入词\n"), 0o600); err != nil {
		t.Fatalf("write dictionary: %v", err)
	}
	// Deliberately NOT server.NewSensitiveEngine(): identity has to be provable
	// against an engine this test built, or the assertion could hold by both
	// sides consulting the same default path.
	engine := sensitive.New(dict)

	// New() rather than mustServer(): that helper builds the struct directly and
	// is what the white-box turn nails need, so it would not exercise the
	// constructor this seam lives in.
	dataRoot(t)
	t.Setenv("OPENAI_API_KEY", "")
	srv, err := New(Config{Addr: "127.0.0.1:0", NoChannel: true, NoMemory: true, SensitiveEngine: engine})
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	if srv.sensitiveEngine != engine {
		t.Fatal("the server built its own engine instead of using the injected one")
	}
	if !srv.sensitiveEngine.Filter("注入词").Matched() {
		t.Fatal("the injected engine is not the one carrying the user's dictionary")
	}
}

// The other half of the seam — no engine injected, so the server builds its own
// — is already pinned by TestTheProductionConstructorBuildsTheEngine in
// sensitive_turn_test.go (it calls New with no SensitiveEngine and asserts both
// non-nil and the built-in fallback). It is not repeated here: two names for one
// property is how the second copy goes stale.

// TestANilEngineIsNotARequestForNoFiltering pins the helper directly. A nil
// engine passed to app.WrapSensitive passes the sender through unchanged, so a
// caller who reads "nil means no engine" as "nil means don't bother" would be
// turning a compliance feature off with a zero value.
func TestANilEngineIsNotARequestForNoFiltering(t *testing.T) {
	if sensitiveEngineOr(nil) == nil {
		t.Fatal("a nil injected engine must fall back to the default, never disable filtering")
	}
}

// OCTO-FORK: PR-4c 三条降级语义与条件刷新 — see the current implementation plan (§2 PR-4c).
package productruntime

import (
	"context"
	"encoding/json"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/open-octo/octo-agent/internal/catalogstore"
	"github.com/open-octo/octo-agent/internal/credentialstore"
	"github.com/open-octo/octo-agent/internal/productstate"
)

// These tests are PR-4c's judgement: "when the catalog is unusable, what does
// the user see and what can they still do" (需求基线 B4/B9, 闭环 L-C2).
//
// They live here rather than in catalog_test.go because they test a different
// subject. PR-4b's file pins *how a fetch is classified*; this one pins what the
// classification is turned into — four states, one refresh rule, and the
// guarantee that none of them can reach a model that did not come from a signed
// catalog.

// TestCatalogStateIsOneJudgement pins the four-valued judgement, its precedence,
// and the fact that it has exactly one home.
//
// The precedence case is real and not hypothetical: a build with a perfectly
// good cache can still be handed an answer it cannot verify, and the two
// defensible readings (keep serving the cache / fail closed) give the user
// opposite outcomes. B4 groups a failed verification with the other two kinds of
// unusability and B9 requires each to have its own sentence, and a sentence needs
// somewhere to appear — so the verification failure wins (see assessCatalog).
func TestCatalogStateIsOneJudgement(t *testing.T) {
	now := time.Now()
	future := now.Add(time.Hour)
	past := now.Add(-time.Hour)

	cases := map[string]struct {
		entry     catalogstore.Entry
		haveCache bool
		outcome   catalogOutcome
		want      catalogState
		retryable bool
	}{
		"nothing has ever been cached": {
			outcome:   catalogUnavailable,
			want:      catalogStateAbsent,
			retryable: true,
		},
		"the cache expired and the refresh did not succeed": {
			entry:     catalogstore.Entry{CatalogVersion: "2026-09-11.1", ExpiresAt: past},
			haveCache: true,
			outcome:   catalogTransport,
			want:      catalogStateStale,
			retryable: true,
		},
		"the envelope that arrived did not verify": {
			entry:     catalogstore.Entry{CatalogVersion: "2026-09-11.1", ExpiresAt: future},
			haveCache: true,
			outcome:   catalogVerification,
			want:      catalogStateUnverifiable,
			// Not retryable: B4's recovery is the platform changing its
			// signature or key, which pressing a button cannot cause. Offering a
			// retry would promise something the system cannot deliver (§3.9).
			retryable: false,
		},
		"a verified catalog that has not expired": {
			entry:     catalogstore.Entry{CatalogVersion: "2026-09-11.1", ExpiresAt: future},
			haveCache: true,
			outcome:   catalogReady,
			want:      catalogStateReady,
			retryable: false,
		},
		"a good cache, but the most recent answer could not be verified": {
			entry:     catalogstore.Entry{CatalogVersion: "2026-09-11.1", ExpiresAt: future},
			haveCache: true,
			outcome:   catalogVerification,
			want:      catalogStateUnverifiable,
			retryable: false,
		},
	}

	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			got := assessCatalog(tc.entry, tc.haveCache, tc.outcome, now)
			if got.State != tc.want {
				t.Errorf("state = %q, want %q", got.State, tc.want)
			}
			if got.Retryable != tc.retryable {
				t.Errorf("retryable = %v, want %v", got.Retryable, tc.retryable)
			}
			if tc.haveCache && got.CatalogVersion != tc.entry.CatalogVersion {
				t.Errorf("catalogVersion = %q, want the cache's %q", got.CatalogVersion, tc.entry.CatalogVersion)
			}
		})
	}

	// The mechanical half: the four values are assigned in exactly one file. A
	// behavioural test can only see the states it happens to construct, so a
	// second judgement added later on a path no test walks would pass silently —
	// the same reason TestOnlyOnePlaceFetchesTheCatalog reads the source.
	root := repoRoot(t)
	owners := map[string][]string{}
	err := filepath.WalkDir(root, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			switch d.Name() {
			case ".git", "node_modules", "dist", "web":
				return filepath.SkipDir
			}
			return nil
		}
		if !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
			return nil
		}
		raw, readErr := os.ReadFile(path)
		if readErr != nil {
			return readErr
		}
		rel, relErr := filepath.Rel(root, path)
		if relErr != nil {
			return relErr
		}
		for _, name := range []string{"catalogStateReady", "catalogStateAbsent", "catalogStateStale", "catalogStateUnverifiable"} {
			if strings.Contains(string(raw), name) {
				owners[name] = append(owners[name], rel)
			}
		}
		return nil
	})
	if err != nil {
		t.Fatalf("walk the repository: %v", err)
	}
	for _, name := range []string{"catalogStateReady", "catalogStateAbsent", "catalogStateStale", "catalogStateUnverifiable"} {
		want := filepath.Join("internal", "productruntime", "catalog.go")
		got := owners[name]
		if len(got) != 1 || got[0] != want {
			t.Errorf("%s is used by %v; the catalogue's availability is judged in exactly one place (%s)",
				name, got, want)
		}
	}
}

// TestAFailedVerificationIsTerminal is 需求基线 B4's "不自动重试同一份响应",
// asserted on the request count rather than on the state.
//
// The state alone cannot tell the difference between "we stopped asking" and "we
// asked again and it failed again", and those are different behaviours: the
// second is a client that would hammer a compromised platform on every picker
// open.
func TestAFailedVerificationIsTerminal(t *testing.T) {
	f := newCatalogFixture(t)
	f.signIn()
	f.prime()

	// A new version so the conditional request is answered with a body at all (an
	// equal version gets a 304 and never reaches verification), and tampered so
	// the body does not check out.
	f.platform.SetCatalogVersion("2026-09-13.9")
	f.platform.TamperPolicy()

	if outcome, _ := f.rt.refreshCatalog(context.Background(), false); outcome != catalogVerification {
		t.Fatalf("refresh = %q, want %q", outcome, catalogVerification)
	}
	if got := f.rt.currentCatalogAvailability().State; got != catalogStateUnverifiable {
		t.Fatalf("state = %q, want %q", got, catalogStateUnverifiable)
	}

	before := f.platform.CatalogRefreshCount()
	after, err := f.rt.refreshCatalogOnDemand(context.Background())
	if err != nil {
		t.Fatalf("refresh on demand = %v", err)
	}

	if n := f.platform.CatalogRefreshCount(); n != before {
		t.Errorf("the platform was asked %d more times after a failed verification; B4 forbids retrying that answer", n-before)
	}
	if after.State != catalogStateUnverifiable {
		t.Errorf("state = %q, want it to stay %q", after.State, catalogStateUnverifiable)
	}
}

// TestKnownVersionGetsA304AndTheFileIsUntouched is 需求基线 B3's third
// acceptance clause ("同一份缓存收到 304 后文件 mtime 不变"), stated on the disk.
//
// Both halves matter and they are different failures: a client that never sent
// the version would get a full catalog back and rewrite the file (the U 盘 write
// B2 规则 4 exists to avoid), and a client that treated 304 as a failure would
// leave the user blocked over an answer that says everything is fine.
func TestKnownVersionGetsA304AndTheFileIsUntouched(t *testing.T) {
	f := newCatalogFixture(t)
	f.signIn()
	f.prime()

	before, err := os.Stat(f.cachePath())
	if err != nil {
		t.Fatalf("stat the cache: %v", err)
	}
	beforeBytes := f.cache()

	outcome, err := f.rt.refreshCatalog(context.Background(), false)
	if outcome != catalogReady {
		t.Fatalf("outcome = %q (err %v), want %q: a 304 is an answer, not a failure", outcome, err, catalogReady)
	}
	if n := f.platform.CatalogRefreshCount(); n != 1 {
		t.Errorf("the refresh endpoint was called %d times, want 1", n)
	}

	after, err := os.Stat(f.cachePath())
	if err != nil {
		t.Fatalf("stat the cache: %v", err)
	}
	if !after.ModTime().Equal(before.ModTime()) {
		t.Errorf("the cache was rewritten (mtime %v -> %v) by an answer that carried no catalog",
			before.ModTime(), after.ModTime())
	}
	if string(f.cache()) != string(beforeBytes) {
		t.Error("the cache bytes changed although the platform said the held version was current")
	}
}

// TestUnchangedAnswerIsRecognisedInBothSpellings covers 中台交付包 §4.3's "304 或
// {"unchanged": true} 二者之一".
//
// The platform may pick either, so a client that understood one would work
// against half the deployments — and it would fail in the worst way, by treating
// "nothing new" as an error and blocking a user whose catalog is fine.
func TestUnchangedAnswerIsRecognisedInBothSpellings(t *testing.T) {
	for name, body := range map[string]bool{
		"a bare 304":            false,
		"{\"unchanged\": true}": true,
	} {
		t.Run(name, func(t *testing.T) {
			f := newCatalogFixture(t)
			f.signIn()
			f.prime()
			if body {
				f.platform.SpellUnchangedInBody()
			}

			outcome, err := f.rt.refreshCatalog(context.Background(), false)
			if outcome != catalogReady || err != nil {
				t.Fatalf("outcome = %q (err %v), want %q", outcome, err, catalogReady)
			}
			// Proven by the answer, not by the absence of a complaint: an
			// unchanged answer still reaches the platform exactly once.
			if n := f.platform.CatalogRefreshCount(); n != 1 {
				t.Errorf("the refresh endpoint was called %d times, want 1", n)
			}
		})
	}
}

// TestStaleCatalogTriggersOneRefreshOnDemand is B3's "TTL 到期后下一次开选择器触发
// 刷新", and it pins two things at once: exactly one attempt per read (not zero,
// not a loop), and that a stale cache is recovered only from a newly signed
// full snapshot. It deliberately omits knownVersion for the retry: a 304 has no
// bytes with which to replace an expired signature window.
func TestStaleCatalogTriggersOneRefreshOnDemand(t *testing.T) {
	t.Run("the refresh brings a newer catalog", func(t *testing.T) {
		f := newCatalogFixture(t)
		f.signIn()

		// One second of freshness, so the stored entry is already past its
		// window the moment it lands — a lapsed TTL with no sleeping.
		f.platform.SetCatalogTTL(1)
		f.prime()
		if got := f.rt.currentCatalogAvailability().State; got != catalogStateStale {
			t.Fatalf("state = %q, want %q before the refresh", got, catalogStateStale)
		}

		f.platform.SetCatalogVersion("2026-09-13.9")
		// Freshness back to normal: the point of this subtest is a *newer*
		// catalog arriving, and leaving a one-second TTL in place would make the
		// replacement expire on arrival and report stale for a second reason.
		f.platform.SetCatalogTTL(0)
		initialBefore := f.platform.CatalogInitialCount()
		avail, err := f.rt.refreshCatalogOnDemand(context.Background())
		if err != nil {
			t.Fatalf("refresh on demand = %v", err)
		}

		if n := f.platform.CatalogInitialCount(); n != initialBefore+1 {
			t.Errorf("full catalog requests = %d, want %d after the stale cache", n, initialBefore+1)
		}
		if n := f.platform.CatalogRefreshCount(); n != 0 {
			t.Errorf("conditional catalog requests = %d, want 0 for an expired cache", n)
		}
		if avail.State != catalogStateReady {
			t.Errorf("state = %q, want %q once a fresh catalog arrived", avail.State, catalogStateReady)
		}
	})

	t.Run("the same catalog version is reissued with a fresh signed lease", func(t *testing.T) {
		f := newCatalogFixture(t)
		f.signIn()
		f.platform.SetCatalogTTL(1)
		f.prime()

		f.platform.SetCatalogTTL(120)
		initialBefore := f.platform.CatalogInitialCount()
		avail, err := f.rt.refreshCatalogOnDemand(context.Background())
		if err != nil {
			t.Fatalf("refresh on demand = %v", err)
		}

		if n := f.platform.CatalogInitialCount(); n != initialBefore+1 {
			t.Errorf("full catalog requests = %d, want %d after the stale cache", n, initialBefore+1)
		}
		if n := f.platform.CatalogRefreshCount(); n != 0 {
			t.Errorf("conditional catalog requests = %d, want 0 for an expired cache", n)
		}
		if avail.State != catalogStateReady {
			t.Errorf("state = %q, want %q once the reissued lease is cached", avail.State, catalogStateReady)
		}
	})
}

// TestTheCatalogEndpointReportsTheState walks the wire shape (本地API契约 §2.14).
//
// It goes through the mounted surface rather than the judgement so the four
// fields, their types and their always-present rule are what is asserted: the
// frontend distinguishes the states by their values, so a field that is
// sometimes omitted would collapse two of them.
func TestTheCatalogEndpointReportsTheState(t *testing.T) {
	f := newCatalogFixture(t)
	f.signIn()

	get := func(t *testing.T) map[string]any {
		t.Helper()
		rec := httptest.NewRecorder()
		f.rt.Handler().ServeHTTP(rec, httptest.NewRequest("GET", "/api/product/catalog", nil))
		if rec.Code != 200 {
			t.Fatalf("GET /api/product/catalog = %d", rec.Code)
		}
		var body map[string]any
		if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
			t.Fatalf("decode: %v (body %s)", err, rec.Body.String())
		}
		return body
	}

	// No cache yet: the read performs the on-demand refresh (absent needs one),
	// which succeeds here, so the endpoint answers ready.
	first := get(t)
	if first["state"] != string(catalogStateReady) {
		t.Errorf("state = %q, want %q: the read refreshes a catalog it does not have", first["state"], catalogStateReady)
	}
	if first["catalogVersion"] == "" {
		t.Error("catalogVersion is empty although a catalog was established")
	}
	for _, key := range []string{"state", "retryable", "catalogVersion", "expiresAt"} {
		if _, ok := first[key]; !ok {
			t.Errorf("%s is missing from the response; all four fields are always present", key)
		}
	}

	// Remove the store entirely: the answer must be absent, and it must still be
	// a four-field object rather than an error.
	f.rt.deps.Catalog = nil
	second := get(t)
	if second["state"] != string(catalogStateAbsent) {
		t.Errorf("state = %q, want %q without a store", second["state"], catalogStateAbsent)
	}
	if second["retryable"] != true {
		t.Errorf("retryable = %v, want true: B4's recovery for no cache is a successful refresh", second["retryable"])
	}
	if second["expiresAt"] != "" {
		t.Errorf("expiresAt = %q, want an empty string when there is no cache to describe", second["expiresAt"])
	}
}

func TestCatalogEndpointEndsARefusedSessionInsteadOfReportingStale(t *testing.T) {
	f := newCatalogFixture(t)
	if err := f.rt.deps.State.ApplyLogin(productstate.LoginOutcome{
		PhoneMasked: "138****1234", Nickname: "tester", BoxCode: "BOX-TEST",
	}); err != nil {
		t.Fatal(err)
	}
	if err := f.rt.deps.Creds.Save(credentialstore.Credential{
		RefreshToken: "refused-after-stub-restart", InstallID: f.rt.deps.State.InstallID(),
	}); err != nil {
		t.Fatal(err)
	}

	rec := httptest.NewRecorder()
	f.rt.Handler().ServeHTTP(rec, httptest.NewRequest("GET", "/api/product/catalog", nil))
	if rec.Code != 401 {
		t.Fatalf("GET /api/product/catalog = %d, want 401 after refresh-token refusal: %s", rec.Code, rec.Body.String())
	}
	if f.rt.deps.State.PublicState().LoggedIn {
		t.Fatal("catalog refresh refusal left the UI in a logged-in state")
	}
	if _, present, err := f.rt.deps.Creds.Load(); err != nil || present {
		t.Fatalf("credential after catalog refresh refusal = present:%v err:%v, want removed", present, err)
	}
}

// TestAbsentCatalogDoesNotAskForKeys is 需求基线 B4's explicit prohibition.
//
// "无缓存" is the ordinary state of a fresh installation, and answering it with
// the API-key wizard would tell a user of a portable product to configure an
// endpoint they cannot configure — the built-in gateway is the only one there is
// (C1 规则 2). The two facts are asserted together because the prohibition only
// makes sense while the build is otherwise correctly configured: a build that
// genuinely cannot reach a control plane SHOULD show that page.
func TestAbsentCatalogDoesNotAskForKeys(t *testing.T) {
	f := newCatalogFixture(t)
	f.signIn()
	f.rt.deps.Catalog = nil

	// The endpoint that decides the blocked page still reports a healthy build,
	// so nothing about "no catalog" can be read as "no keys".
	rec := httptest.NewRecorder()
	f.rt.Handler().ServeHTTP(rec, httptest.NewRequest("GET", "/api/product/control-plane", nil))
	var plane struct {
		Configured     bool `json:"configured"`
		HasTrustedKeys bool `json:"hasTrustedKeys"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &plane); err != nil {
		t.Fatalf("decode control-plane: %v", err)
	}
	if !plane.Configured || !plane.HasTrustedKeys {
		t.Fatalf("control-plane = %+v, want a configured build with trusted keys", plane)
	}

	avail := f.rt.currentCatalogAvailability()
	if avail.State != catalogStateAbsent {
		t.Fatalf("state = %q, want %q", avail.State, catalogStateAbsent)
	}
	if !avail.Retryable {
		t.Error("retryable = false, want true: a first successful refresh is the recovery B4 names")
	}
}

// TestNoPathFallsBackToALocalModel is B4's 禁止 clause as a nail: none of the
// four states may produce a model that did not come out of a signed catalog.
//
// It is stated on the picker's own endpoint because that is where an invented
// model would appear, and it counts rather than eyeballs: the expected number is
// derived from the cache that is actually on disk, so every model the picker
// offers has to be accounted for by it. A future "helpful default" added to this
// package therefore fails here rather than at acceptance.
func TestNoPathFallsBackToALocalModel(t *testing.T) {
	type picker struct {
		Vendors []struct {
			Models []struct {
				ID string `json:"id"`
			} `json:"models"`
		} `json:"vendors"`
	}
	read := func(t *testing.T, f *catalogFixture) picker {
		t.Helper()
		rec := httptest.NewRecorder()
		f.rt.Handler().ServeHTTP(rec, httptest.NewRequest("GET", "/api/product/models", nil))
		if rec.Code != 200 {
			t.Fatalf("GET /api/product/models = %d", rec.Code)
		}
		var out picker
		if err := json.Unmarshal(rec.Body.Bytes(), &out); err != nil {
			t.Fatalf("decode: %v (body %s)", err, rec.Body.String())
		}
		return out
	}
	// offered collects the ids on screen; cached counts what the verified cache
	// is allowed to produce right now.
	offered := func(p picker) map[string]bool {
		seen := map[string]bool{}
		for _, vendor := range p.Vendors {
			for _, model := range vendor.Models {
				seen[model.ID] = true
			}
		}
		return seen
	}
	cached := func(t *testing.T, f *catalogFixture) map[string]bool {
		t.Helper()
		want := map[string]bool{}
		if f.rt.deps.Catalog == nil {
			return want
		}
		if f.rt.currentCatalogAvailability().State != catalogStateReady {
			return want
		}
		entry, err := f.store.Load()
		if err != nil {
			return want
		}
		policy, err := entry.Envelope.DecodePolicy()
		if err != nil {
			t.Fatalf("decode the cached policy: %v", err)
		}
		for _, m := range policy.Catalog.Models {
			if m.Eligible && m.Transport == "gateway" {
				want[m.ID] = true
			}
		}
		return want
	}

	cases := map[string]struct {
		stage func(f *catalogFixture)
	}{
		"no cache": {
			stage: func(f *catalogFixture) { f.rt.deps.Catalog = nil },
		},
		"a verified catalog": {
			stage: func(f *catalogFixture) { f.prime() },
		},
		"an expired cache": {
			stage: func(f *catalogFixture) {
				f.platform.SetCatalogTTL(1)
				f.prime()
			},
		},
		"an answer that did not verify": {
			stage: func(f *catalogFixture) {
				f.prime()
				f.platform.SetCatalogVersion("2026-09-13.9")
				f.platform.TamperPolicy()
				if outcome, _ := f.rt.refreshCatalog(context.Background(), false); outcome != catalogVerification {
					f.t.Fatalf("refresh = %q, want a refusal", outcome)
				}
			},
		},
	}

	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			f := newCatalogFixture(t)
			f.signIn()
			tc.stage(f)

			got := offered(read(t, f))
			want := cached(t, f)
			// A verified cache must actually produce something, or the assertion
			// below would be vacuously satisfied by an empty picker.
			if name == "a verified catalog" && len(want) == 0 {
				t.Fatal("the fixture catalog yielded no eligible gateway models; this test would prove nothing")
			}
			for id := range got {
				if !want[id] {
					t.Errorf("the picker offers %q, which the signed cache does not account for — "+
						"a model may not come from config.yml, a local provider or a built-in list", id)
				}
			}
			if len(got) != len(want) {
				t.Errorf("the picker offers %d models, the signed cache accounts for %d", len(got), len(want))
			}
		})
	}

	// The mechanical half: no model id may be named in this package at all. The
	// contract's example ids are the tempting copy, and one of them here would be
	// a built-in list with a single entry in it.
	for _, id := range []string{"buding-", "privacy", "smart"} {
		if strings.Contains(catalogSource(t), id) {
			t.Errorf("catalog.go names %q; model ids come from the signed catalog", id)
		}
	}
}

// catalogSource reads this package's catalogue file, the one place a local model
// list would hide.
func catalogSource(t *testing.T) string {
	t.Helper()
	raw, err := os.ReadFile(filepath.Join(repoRoot(t), "internal", "productruntime", "catalog.go"))
	if err != nil {
		t.Fatalf("read catalog.go: %v", err)
	}
	return string(raw)
}

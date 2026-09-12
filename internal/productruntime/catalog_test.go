package productruntime

import (
	"context"
	"errors"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"github.com/open-octo/octo-agent/internal/brand"
	"github.com/open-octo/octo-agent/internal/catalogstore"
	"github.com/open-octo/octo-agent/internal/credentialstore"
	"github.com/open-octo/octo-agent/internal/productclient"
	"github.com/open-octo/octo-agent/internal/productclient/clienttest"
	"github.com/open-octo/octo-agent/internal/productstate"
)

// These tests live in the package because the subject is the fetch-and-verify
// step and the outcome it reports, both unexported on purpose: the four
// outcomes exist for PR-4c to consume, and no caller outside this package has
// any business deciding what they mean.
//
// PR-4b is also where L-A6 becomes reachable end to end, because bootstrap is
// the first authorised platform call any production code makes. The mounted
// tests (mount_test.go) walk that road; this file pins the outcomes.

type catalogFixture struct {
	t        *testing.T
	rt       *Runtime
	platform *clienttest.Server
	store    *catalogstore.Store
	root     string
}

const catalogTestPhone = "13800001234"

func newCatalogFixture(t *testing.T) *catalogFixture {
	t.Helper()
	root := t.TempDir()
	t.Setenv("OCTO_DATA_ROOT", root)

	platform := clienttest.New()
	srv := httptest.NewServer(platform.Handler())
	t.Cleanup(srv.Close)

	state, err := productstate.Open(productstate.Options{})
	if err != nil {
		t.Fatalf("productstate.Open: %v", err)
	}
	creds, err := credentialstore.Open(credentialstore.Options{})
	if err != nil {
		t.Fatalf("credentialstore.Open: %v", err)
	}
	store, err := catalogstore.Open(catalogstore.Options{})
	if err != nil {
		t.Fatalf("catalogstore.Open: %v", err)
	}

	client := productclient.New(srv.URL+"/v1", productclient.ClientMeta{
		Version:   "test",
		Platform:  "test",
		Arch:      "test",
		InstallID: state.InstallID(),
	}, &productclient.CredentialHolder{})

	rt := New(Deps{
		State:    state,
		Creds:    creds,
		Platform: client,
		Catalog:  store,
		// Explicit fixture facts, not productprofile.Current(): that value is
		// chosen by a build tag, and a production-tagged test run would make
		// these tests fail for a reason that has nothing to do with them.
		CatalogTrust: CatalogTrust{
			TrustedKeys: map[string]string{clienttest.FixtureSigningKeyID: clienttest.FixtureSigningPublicKey()},
			// The audience the build actually ships, read from its single owner,
			// so a rebrand breaks this test rather than the developer build.
			Audience: brand.Load().BrandID,
		},
		ControlPlane: ControlPlaneStatus{Configured: true, HasTrustedKeys: true},
	})

	return &catalogFixture{t: t, rt: rt, platform: platform, store: store, root: root}
}

// signIn performs the platform half of a login, which is what leaves the client
// holding a token the stand-in recognises. fetchCatalog only ever runs behind a
// session, so every test needs one.
func (f *catalogFixture) signIn() {
	f.t.Helper()
	ctx := context.Background()
	if _, err := f.rt.deps.Platform.SendSMS(ctx, productclient.SendSMSRequest{Phone: catalogTestPhone}); err != nil {
		f.t.Fatalf("SendSMS: %v", err)
	}
	if _, err := f.rt.deps.Platform.Login(ctx, productclient.LoginRequest{
		Phone:          catalogTestPhone,
		Code:           clienttest.FixtureSMSCode,
		ActivationCode: clienttest.FixtureActivationCode,
		BoxCode:        clienttest.FixtureBoxCode,
	}); err != nil {
		f.t.Fatalf("Login: %v", err)
	}
}

func (f *catalogFixture) fetch() (catalogOutcome, error) {
	f.t.Helper()
	return f.rt.fetchCatalog(context.Background())
}

func (f *catalogFixture) cachePath() string { return filepath.Join(f.root, "catalog.json") }

func (f *catalogFixture) cache() []byte {
	f.t.Helper()
	raw, err := os.ReadFile(f.cachePath())
	if err != nil {
		f.t.Fatalf("read catalog.json: %v", err)
	}
	return raw
}

func (f *catalogFixture) cacheExists() bool {
	_, err := os.Stat(f.cachePath())
	return err == nil
}

// TestCatalogOutcomesAreDistinguishable is the deliverable 开发计划 PR-4b 缺口 ⑥
// asks for: PR-4c has to tell four situations apart to word them and to decide
// whether to open a turn, so the fetch reports which one happened. Nothing here
// decides any wording - that is 4c's, and keeping the two apart is what stops
// this PR from becoming a UI change.
func TestCatalogOutcomesAreDistinguishable(t *testing.T) {
	cases := map[string]struct {
		inject func(f *catalogFixture)
		want   catalogOutcome
	}{
		"a signed catalog is fetched, verified and cached": {
			inject: func(*catalogFixture) {},
			want:   catalogReady,
		},
		"the platform answered 5xx": {
			inject: func(f *catalogFixture) {
				f.platform.FailBootstrap(502, productclient.CodeUpstreamUnavailable)
			},
			want: catalogTransport,
		},
		"the signature does not cover what arrived": {
			inject: func(f *catalogFixture) { f.platform.TamperPolicy() },
			want:   catalogVerification,
		},
		"a validly signed but older catalog": {
			inject: func(f *catalogFixture) {
				if _, err := f.fetch(); err != nil {
					f.t.Fatalf("priming fetch: %v", err)
				}
				f.platform.SetCatalogVersion("2026-09-01.0")
			},
			want: catalogRolledBack,
		},
		"bootstrap carries no signed policy": {
			inject: func(f *catalogFixture) { f.platform.OmitPolicy() },
			want:   catalogUnavailable,
		},
	}

	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			f := newCatalogFixture(t)
			f.signIn()
			tc.inject(f)

			got, err := f.fetch()
			if got != tc.want {
				t.Errorf("outcome = %q (err %v), want %q", got, err, tc.want)
			}
			if tc.want == catalogReady && err != nil {
				t.Errorf("a ready catalog reported an error: %v", err)
			}
		})
	}
}

// TestAFailedFetchLeavesTheCacheAlone is 需求基线 B2 规则 2's fail-closed half,
// stated on the disk rather than in the return value: a refusal that still
// overwrites the cache is not a refusal, and it would destroy the only catalog
// the user can fall back on (PR-4c's "陈旧" case).
func TestAFailedFetchLeavesTheCacheAlone(t *testing.T) {
	cases := map[string]func(f *catalogFixture){
		"the signature does not verify":   func(f *catalogFixture) { f.platform.TamperPolicy() },
		"the platform is unreachable":     func(f *catalogFixture) { f.platform.FailBootstrap(502, productclient.CodeUpstreamUnavailable) },
		"the version rolls back":          func(f *catalogFixture) { f.platform.SetCatalogVersion("2026-09-01.0") },
		"the envelope is missing":         func(f *catalogFixture) { f.platform.OmitPolicy() },
		"the audience is another product": func(f *catalogFixture) { f.platform.SetPolicyAudience("someone-else") },
	}

	for name, inject := range cases {
		t.Run(name, func(t *testing.T) {
			f := newCatalogFixture(t)
			f.signIn()

			if outcome, err := f.fetch(); outcome != catalogReady {
				t.Fatalf("priming fetch = %q (err %v), want a cached catalog first", outcome, err)
			}
			before := f.cache()

			inject(f)
			outcome, err := f.fetch()
			if outcome == catalogReady {
				t.Fatalf("outcome = ready, want a refusal (err %v)", err)
			}
			if string(f.cache()) != string(before) {
				t.Error("the cache was overwritten by a fetch that was refused")
			}
		})
	}
}

// TestNoCatalogStoreMeansNoFetch covers the nil-dependency edge: without a store
// there is nowhere to put a catalog, and fetching one would be a network call
// whose result nothing can read. The outcome still has to be reported, so a
// caller cannot mistake "we never tried" for "we succeeded".
func TestNoCatalogStoreMeansNoFetch(t *testing.T) {
	f := newCatalogFixture(t)
	f.signIn()

	f.rt.deps.Catalog = nil
	outcome, err := f.fetch()

	if outcome != catalogUnavailable {
		t.Errorf("outcome = %q (err %v), want %q", outcome, err, catalogUnavailable)
	}
	if n := f.platform.BootstrapCount(); n != 0 {
		t.Errorf("the platform was asked %d times without a store to keep the answer", n)
	}
}

// TestOnlyOnePlaceFetchesTheCatalog is 开发计划 PR-4b 缺口 ④ as a mechanical nail.
//
// "Startup refreshes the catalog" and "login refreshes the catalog" are both
// plausible readings of the contract, and implementing both gives two caches
// racing on one file with no owner. The rule is that exactly one place starts a
// fetch, and the cheapest way to defend a rule like that is to look.
//
// It reads the source rather than the behaviour because a behavioural test can
// only observe the call sites it happens to exercise - a third one added later,
// on a path no test walks, would pass silently. The definition in productclient
// is not a match: the pattern requires a receiver, so `) Bootstrap(` does not
// count.
func TestOnlyOnePlaceFetchesTheCatalog(t *testing.T) {
	root := repoRoot(t)
	allowed := map[string]bool{
		filepath.Join("internal", "productruntime", "catalog.go"): true,
	}
	call := regexp.MustCompile(`\.Bootstrap\(`)

	var found []string
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
		rel, relErr := filepath.Rel(root, path)
		if relErr != nil {
			return relErr
		}
		if !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
			return nil
		}
		raw, readErr := os.ReadFile(path)
		if readErr != nil {
			return readErr
		}
		if call.Match(raw) && !allowed[rel] {
			found = append(found, rel)
		}
		return nil
	})
	if err != nil {
		t.Fatalf("walk the repository: %v", err)
	}
	if len(found) > 0 {
		t.Errorf("the catalog is fetched from %v; it must be fetched in exactly one place (%v)",
			found, "internal/productruntime/catalog.go")
	}

	// The walk has to have found the file it is supposed to protect, or the
	// assertion above would pass by finding nothing at all.
	raw, err := os.ReadFile(filepath.Join(root, "internal", "productruntime", "catalog.go"))
	if err != nil {
		t.Fatalf("read the single fetch point: %v", err)
	}
	if !call.Match(raw) {
		t.Error("internal/productruntime/catalog.go no longer fetches the catalog; this nail is not testing anything")
	}
}

// repoRoot locates the module root from this package's directory, so the walk
// above does not depend on the test's working directory staying where it is.
func repoRoot(t *testing.T) string {
	t.Helper()
	out, err := exec.Command("go", "env", "GOMOD").Output()
	if err != nil {
		t.Skipf("go env GOMOD unavailable: %v", err)
	}
	gomod := strings.TrimSpace(string(out))
	if gomod == "" || gomod == os.DevNull {
		t.Skip("not running inside a module")
	}
	return filepath.Dir(gomod)
}

// A refusal must be reported as an error as well as an outcome, or a caller that
// logs only the error would see a silent success.
func TestARefusalCarriesTheUnderlyingError(t *testing.T) {
	f := newCatalogFixture(t)
	f.signIn()
	f.platform.TamperPolicy()

	outcome, err := f.fetch()
	if outcome != catalogVerification {
		t.Fatalf("outcome = %q, want %q", outcome, catalogVerification)
	}
	if err == nil {
		t.Fatal("a refused catalog was reported without an error")
	}
	if !errors.Is(err, productclient.ErrBadSignature) {
		t.Errorf("error = %v, want it to name the signature failure", err)
	}
}

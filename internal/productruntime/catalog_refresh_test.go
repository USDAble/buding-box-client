package productruntime

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/open-octo/octo-agent/internal/catalogstore"
	"github.com/open-octo/octo-agent/internal/productclient"
)

// OCTO-FORK: picker checks are conditional, shared, and never invalidate a
// usable cache just because an operational status check is slow or offline.
func TestPickerRevalidationKeepsCachedRowsVisibleAndUpdatesEligibility(t *testing.T) {
	f, tokens, started, release, calls := heldCatalog(t)
	envelope, err := f.platform.Policy()
	if err != nil {
		t.Fatal(err)
	}
	policy, _ := envelope.DecodePolicy()
	if err := f.store.Put(catalogstore.Entry{
		CatalogVersion: policy.Catalog.Version, Envelope: envelope,
		FetchedAt: time.Now(), ExpiresAt: time.Now().Add(5 * time.Minute),
	}); err != nil {
		t.Fatal(err)
	}
	f.rt.catalogSession, f.rt.catalogSessionVerified = tokens.SessionGeneration(), true
	f.platform.SetCatalogVersion("2026-10-20.1")
	f.platform.SetModelIneligible("buding-cloud-fast")
	done := make(chan *httptest.ResponseRecorder, 1)
	go func() {
		rec := httptest.NewRecorder()
		f.rt.handleCatalog(rec, httptest.NewRequest(http.MethodGet, "/api/product/catalog?revalidate=1", nil))
		done <- rec
	}()
	awaitCatalogStarted(t, started)
	readEligibility := func() bool {
		t.Helper()
		rec := httptest.NewRecorder()
		f.rt.handleModels(rec, httptest.NewRequest(http.MethodGet, "/api/product/models", nil))
		var rows modelsDTO
		if err := json.Unmarshal(rec.Body.Bytes(), &rows); err != nil || rows.State != "ready" {
			t.Fatalf("cached rows unavailable during refresh: %s %v", rec.Body, err)
		}
		for _, vendor := range rows.Vendors {
			for _, model := range vendor.Models {
				if model.ID == "buding-cloud-fast" {
					return model.Eligible
				}
			}
		}
		t.Fatal("model was silently removed")
		return false
	}
	if !readEligibility() {
		t.Fatal("pending status check hid a valid cached model")
	}
	f.rt.catalogMu.Lock()
	flight := f.rt.catalogFlight
	f.rt.catalogMu.Unlock()
	if f.rt.startCatalogRefresh(false, false, true) != flight {
		t.Fatal("picker did not share the existing refresh")
	}
	release()
	select {
	case rec := <-done:
		if rec.Code != http.StatusOK {
			t.Fatalf("picker refresh: %d %s", rec.Code, rec.Body)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("picker refresh did not finish")
	}
	if readEligibility() || calls.Load() != 1 || f.platform.CatalogRefreshCount() != 1 {
		t.Fatal("changed eligibility was not installed by one conditional request")
	}
}

func allowPickerRevalidation(f *catalogFixture) {
	f.rt.catalogMu.Lock()
	f.rt.catalogRetryAfter = time.Now().Add(-time.Second)
	f.rt.catalogMu.Unlock()
}

func TestPickerRevalidationThrottlesReopeningAfterUnchangedCatalog(t *testing.T) {
	f := newCatalogFixture(t)
	f.signIn()
	f.prime()
	allowPickerRevalidation(f)
	before, _ := f.store.Load()
	for range 10 {
		if availability, err := f.rt.refreshCatalogForPicker(context.Background()); err != nil || !availability.ready() {
			t.Fatalf("picker: %+v %v", availability, err)
		}
	}
	after, _ := f.store.Load()
	if f.platform.CatalogRefreshCount() != 1 || !after.ExpiresAt.Equal(before.ExpiresAt) {
		t.Fatal("reopening repeated a request or extended the signed lease after 304")
	}
}

func TestPickerRevalidationFailureKeepsOnlyStillValidCache(t *testing.T) {
	f := newCatalogFixture(t)
	f.signIn()
	f.prime()
	allowPickerRevalidation(f)
	before, _ := f.store.Load()
	f.platform.FailCatalog(http.StatusBadGateway, productclient.CodeUpstreamUnavailable)
	if availability, err := f.rt.refreshCatalogForPicker(context.Background()); err == nil || !availability.ready() {
		t.Fatalf("valid cache lost after transport failure: %+v %v", availability, err)
	}
	after, _ := f.store.Load()
	if !after.ExpiresAt.Equal(before.ExpiresAt) || !after.FetchedAt.Equal(before.FetchedAt) {
		t.Fatal("failed refresh modified the lease")
	}
	if _, err := f.rt.refreshCatalogForPicker(context.Background()); err != nil || f.platform.CatalogRefreshCount() != 1 {
		t.Fatal("failed background check was immediately retried")
	}
}

func TestPickerRevalidationDoesNotRetryOrUseFailedSignature(t *testing.T) {
	f := newCatalogFixture(t)
	f.signIn()
	f.prime()
	allowPickerRevalidation(f)
	f.platform.SetCatalogVersion("2026-10-20.2")
	f.platform.TamperPolicy()
	if availability, err := f.rt.refreshCatalogForPicker(context.Background()); err == nil || availability.State != catalogStateUnverifiable {
		t.Fatalf("signature failure not closed: %+v %v", availability, err)
	}
	if availability, _ := f.rt.refreshCatalogForPicker(context.Background()); availability.State != catalogStateUnverifiable || f.platform.CatalogRefreshCount() != 1 {
		t.Fatal("reopening retried failed verification")
	}
}

// Delay the first signed response, not login or later accounts' requests.
func heldCatalog(t *testing.T) (*catalogFixture, *productclient.CredentialHolder, <-chan struct{}, func(), *atomic.Int32) {
	t.Helper()
	f := newCatalogFixture(t)
	started, release := make(chan struct{}), make(chan struct{})
	var once sync.Once
	unblock := func() { once.Do(func() { close(release) }) }
	var calls atomic.Int32
	upstream := f.platform.Handler()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/v1/catalog/models" && calls.Add(1) == 1 {
			recorded := httptest.NewRecorder()
			upstream.ServeHTTP(recorded, r)
			close(started)
			select {
			case <-release:
			case <-r.Context().Done():
				return
			}
			for name, values := range recorded.Header() {
				w.Header()[name] = values
			}
			w.WriteHeader(recorded.Code)
			_, _ = w.Write(recorded.Body.Bytes())
			return
		}
		upstream.ServeHTTP(w, r)
	}))
	t.Cleanup(srv.Close)
	t.Cleanup(unblock)
	tokens := &productclient.CredentialHolder{}
	f.rt.deps.Platform = productclient.New(srv.URL+"/v1", productclient.ClientMeta{}, tokens)
	f.signIn()
	return f, tokens, started, unblock, &calls
}

func awaitCatalogStarted(t *testing.T, started <-chan struct{}) {
	t.Helper()
	select {
	case <-started:
	case <-time.After(3 * time.Second):
		t.Fatal("catalog request did not start")
	}
}

func TestCatalogRefreshSharesRequestAndCancellationOnlyStopsWaiter(t *testing.T) {
	f, _, started, release, calls := heldCatalog(t)
	first := f.rt.startCatalogRefresh(false, true, false)
	awaitCatalogStarted(t, started)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := waitCatalogRefresh(ctx, first); !errors.Is(err, context.Canceled) {
		t.Fatalf("cancelled wait: %v", err)
	}
	for range 12 {
		if got := f.rt.startCatalogRefresh(true, false, false); got != first {
			t.Fatal("same session created a second refresh")
		}
	}
	release()
	if outcome, err := waitCatalogRefresh(context.Background(), first); err != nil || outcome != catalogReady {
		t.Fatalf("shared result: %s %v", outcome, err)
	}
	if calls.Load() != 1 {
		t.Fatalf("upstream requests = %d", calls.Load())
	}
	// Initialization, a menu read, and the send guard all reuse the accepted lease.
	if availability, err := f.rt.refreshCatalogForInitialization(context.Background()); err != nil || !availability.ready() {
		t.Fatalf("initialization = %v %v", availability, err)
	}
	_, _ = f.rt.refreshCatalogOnDemand(context.Background())
	_, _ = f.rt.PrepareCatalogModel(context.Background(), "any-model")
	if calls.Load() != 1 {
		t.Fatalf("valid cache caused %d requests", calls.Load())
	}
}

func TestCatalogRefreshDiscardsPreviousSessionResponse(t *testing.T) {
	for _, nextSession := range []bool{false, true} {
		t.Run(map[bool]string{false: "logout", true: "new login"}[nextSession], func(t *testing.T) {
			f, tokens, started, release, calls := heldCatalog(t)
			old := f.rt.startCatalogRefresh(false, true, false)
			awaitCatalogStarted(t, started)
			held := tokens.Get()
			tokens.Clear()
			if nextSession {
				tokens.Set(held)
				f.platform.SetCatalogVersion("2026-09-13.9")
				if outcome, err := f.rt.refreshCatalog(context.Background(), false); err != nil || outcome != catalogReady {
					t.Fatalf("new account refresh: %s %v", outcome, err)
				}
			}
			release()
			if _, err := waitCatalogRefresh(context.Background(), old); !errors.Is(err, productclient.ErrSessionChanged) {
				t.Fatalf("old request = %v", err)
			}
			entry, err := f.store.Load()
			if nextSession {
				if err != nil || entry.CatalogVersion != "2026-09-13.9" || !f.rt.currentCatalogAvailability().ready() {
					t.Fatalf("new catalog overwritten: %+v %v", entry, err)
				}
				if calls.Load() != 2 {
					t.Fatalf("new session must not join old request: %d calls", calls.Load())
				}
			} else if err == nil {
				t.Fatal("logged-out response was written to cache")
			}
		})
	}
}

func TestCatalogNewSessionCannotUsePreviousCacheAfterRefreshFailure(t *testing.T) {
	f, tokens, _, release, _ := heldCatalog(t)
	release()
	f.prime()
	tokens.Set(tokens.Get())
	f.platform.FailCatalog(http.StatusBadGateway, productclient.CodeUpstreamUnavailable)
	// A new account must not immediately receive the previous account's rows.
	if f.rt.currentCatalogAvailability().ready() {
		t.Fatal("previous account cache remained usable")
	}
	if _, err := f.rt.refreshCatalog(context.Background(), false); err == nil {
		t.Fatal("expected refresh failure")
	}
	if f.rt.currentCatalogAvailability().ready() {
		t.Fatal("old account cache became usable after refresh failure")
	}
}

func TestCatalogRenewalWindow(t *testing.T) {
	now := time.Now()
	if catalogRenewalDue(now.Add(time.Minute), now.Add(-4*time.Minute), now) {
		t.Fatal("renewal too early")
	}
	if !catalogRenewalDue(now.Add(20*time.Second), now.Add(-4*time.Minute), now) {
		t.Fatal("renewal not due before expiry")
	}
	if catalogRenewalDue(now.Add(8*time.Second), now.Add(-2*time.Second), now) {
		t.Fatal("short lease is always renewing")
	}
}

func TestNearExpiryCatalogReturnsImmediatelyWhileRenewalIsShared(t *testing.T) {
	f, tokens, started, release, calls := heldCatalog(t)
	envelope, err := f.platform.Policy()
	if err != nil {
		t.Fatal(err)
	}
	policy, err := envelope.DecodePolicy()
	if err != nil {
		t.Fatal(err)
	}
	if err := f.store.Put(catalogstore.Entry{
		CatalogVersion: policy.Catalog.Version, Envelope: envelope,
		FetchedAt: time.Now().Add(-2 * time.Minute), ExpiresAt: time.Now().Add(20 * time.Second),
	}); err != nil {
		t.Fatal(err)
	}
	f.rt.catalogSession = tokens.SessionGeneration()
	f.rt.catalogSessionVerified = true
	f.platform.SetCatalogVersion("2026-09-13.10")
	availability, err := f.rt.refreshCatalogOnDemand(context.Background())
	if err != nil || !availability.ready() {
		t.Fatalf("usable cache blocked: %v %v", availability, err)
	}
	awaitCatalogStarted(t, started)
	availability, err = f.rt.refreshCatalogOnDemand(context.Background())
	if err != nil || !availability.ready() {
		t.Fatalf("second cache read blocked: %v %v", availability, err)
	}
	f.rt.catalogMu.Lock()
	flight := f.rt.catalogFlight
	f.rt.catalogMu.Unlock()
	release()
	if outcome, err := waitCatalogRefresh(context.Background(), flight); outcome != catalogReady || err != nil {
		t.Fatalf("renewal: %s %v", outcome, err)
	}
	if calls.Load() != 1 || f.platform.CatalogRefreshCount() != 0 {
		t.Fatal("renewal duplicated or used conditional 304")
	}
	entry, _ := f.store.Load()
	if entry.CatalogVersion != "2026-09-13.10" || time.Until(entry.ExpiresAt) < time.Minute {
		t.Fatal("new signed lease was not installed")
	}
}

func TestRestartDoesNotAdoptPreviousAccountsDiskCatalog(t *testing.T) {
	f := newCatalogFixture(t)
	f.signIn()
	f.prime()
	// A restart has an authenticated holder but no proof that the retained file
	// was fetched for it. No startup path may silently claim that file.
	restarted := New(f.rt.deps)
	if restarted.currentCatalogAvailability().ready() {
		t.Fatal("restart adopted unbound disk snapshot")
	}
	f.platform.FailCatalog(http.StatusBadGateway, productclient.CodeUpstreamUnavailable)
	if _, err := restarted.refreshCatalogOnDemand(context.Background()); err == nil {
		t.Fatal("expected offline refresh failure")
	}
	if restarted.currentCatalogAvailability().ready() {
		t.Fatal("offline restart opened old account catalog")
	}
	f.platform.FailCatalog(0, "")
	f.platform.SetCatalogVersion("2026-09-13.10")
	if availability, err := restarted.refreshCatalogOnDemand(context.Background()); err != nil || !availability.ready() {
		t.Fatalf("fresh account proof: %v %v", availability, err)
	}
}

func TestRestartRejectsUnchangedResponseWithoutVerifiedCurrentSession(t *testing.T) {
	f := newCatalogFixture(t)
	f.signIn()
	f.prime()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(http.StatusNotModified) }))
	defer server.Close()
	deps := f.rt.deps
	deps.Platform = productclient.New(server.URL, productclient.ClientMeta{}, signedIn("fixture"))
	restarted := New(deps)
	if _, err := restarted.refreshCatalog(context.Background(), false); err == nil {
		t.Fatal("unconditional 304 approved old account cache")
	}
	if got := restarted.currentCatalogAvailability(); got.ready() || got.Retryable {
		t.Fatalf("missing proof accepted: %v", got)
	}
}

func TestSameVersionResponseCannotApproveTamperedDiskExpiry(t *testing.T) {
	f := newCatalogFixture(t)
	f.signIn()
	f.prime()
	entry, _ := f.store.Load()
	entry.ExpiresAt = time.Now().Add(24 * time.Hour)
	if err := f.store.Put(entry); err != nil {
		t.Fatal(err)
	}
	restarted := New(f.rt.deps)
	if _, err := restarted.refreshCatalog(context.Background(), false); err == nil {
		t.Fatal("same-version no-op approved forged cache expiry")
	}
	if restarted.currentCatalogAvailability().ready() {
		t.Fatal("forged expiry was usable")
	}
}

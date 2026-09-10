package productclient

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
)

// stubTokens is the transport's view of a product session under test. Its
// fields are guarded because the transport reads the token from whichever
// goroutine is sending, which is exactly the concurrency -race is here for.
type stubTokens struct {
	mu         sync.Mutex
	token      string
	refreshErr error
	refreshes  int
	lost       []error
	// refreshDelay widens the window in which concurrent 401s pile up on the
	// single-flight, so the coalescing test does not depend on scheduling luck.
	refreshDelay time.Duration
}

func (s *stubTokens) AccessToken() string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.token
}

func (s *stubTokens) Refresh(context.Context) (string, error) {
	if s.refreshDelay > 0 {
		time.Sleep(s.refreshDelay)
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.refreshes++
	if s.refreshErr != nil {
		return "", s.refreshErr
	}
	s.token = "fresh"
	return "fresh", nil
}

func (s *stubTokens) AuthLost(err error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.lost = append(s.lost, err)
}

func (s *stubTokens) counts() (int, int) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.refreshes, len(s.lost)
}

func okBody(t *testing.T, data any) []byte {
	t.Helper()
	raw, err := json.Marshal(data)
	if err != nil {
		t.Fatal(err)
	}
	env, err := json.Marshal(map[string]any{"data": json.RawMessage(raw), "requestId": "r1"})
	if err != nil {
		t.Fatal(err)
	}
	return env
}

// newTestTransport wires a Transport against a test server. Plane-text localhost
// is the developer/test escape hatch, and sleeping is stubbed so no test pays
// for the backoff schedule.
func newTestTransport(t *testing.T, url string, tokens TokenSource, sleep func(context.Context, time.Duration) error) *Transport {
	t.Helper()
	tr, err := NewTransport(TransportConfig{
		BaseURL:       url,
		ClientVersion: "9.9.9",
		InstallID:     "install-abc",
		Locale:        "zh-CN",
		Platform:      "windows",
		Arch:          "amd64",
		Tokens:        tokens,
		AllowInsecure: true,
		Sleep:         sleep,
	})
	if err != nil {
		t.Fatalf("NewTransport: %v", err)
	}
	return tr
}

func noSleep(context.Context, time.Duration) error { return nil }

// ----- configuration -----

func TestBaseURLMustBeHTTPSForProduction(t *testing.T) {
	cases := []struct {
		name    string
		url     string
		allow   bool
		wantErr bool
	}{
		{"https is always fine", "https://api.example.com/v1", false, false},
		{"plaintext needs the explicit escape hatch", "http://127.0.0.1:8080", false, true},
		{"plaintext allowed for developer/test", "http://127.0.0.1:8080", true, false},
		{"empty", "", true, true},
		{"relative", "/v1", true, true},
		{"unsupported scheme", "ftp://api.example.com", true, true},
		{"credentials in the URL", "https://user:pw@api.example.com", true, true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := NewTransport(TransportConfig{
				BaseURL: tc.url, ClientVersion: "1", InstallID: "i", AllowInsecure: tc.allow,
			})
			if (err != nil) != tc.wantErr {
				t.Fatalf("err = %v, wantErr = %v", err, tc.wantErr)
			}
		})
	}
}

// TestTransportRefusesAnAnonymousIdentity pins the "no silent defaults" rule: an
// empty InstallID would lose per-install rate limiting and make a support
// ticket unattributable, and nothing downstream could tell it had happened.
func TestTransportRefusesAnAnonymousIdentity(t *testing.T) {
	for _, tc := range []struct {
		name    string
		version string
		install string
	}{
		{"no client version", "", "i"},
		{"no install id", "1", ""},
		{"whitespace is not a value", "1", "   "},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if _, err := NewTransport(TransportConfig{BaseURL: "https://a.example.com", ClientVersion: tc.version, InstallID: tc.install}); err == nil {
				t.Fatal("err = nil, want a refusal")
			}
		})
	}
}

// TestTimeoutAndAttemptTables pins 01 §4's per-endpoint table. The write case is
// the one that matters: an automatic retry of SendCode is a second SMS the user
// did not ask for.
func TestTimeoutAndAttemptTables(t *testing.T) {
	if got := timeoutFor(CallWrite); got != 10*time.Second {
		t.Errorf("CallWrite timeout = %v, want 10s", got)
	}
	if got := timeoutFor(CallControl); got != 15*time.Second {
		t.Errorf("CallControl timeout = %v, want 15s", got)
	}
	if got := timeoutFor(CallRead); got != 10*time.Second {
		t.Errorf("CallRead timeout = %v, want 10s", got)
	}
	if got := maxAttemptsFor(CallWrite); got != 1 {
		t.Errorf("CallWrite attempts = %d, want 1 (never auto-retried)", got)
	}
	if got := maxAttemptsFor(CallControl); got != 3 {
		t.Errorf("CallControl attempts = %d, want 3", got)
	}
	if got := maxAttemptsFor(CallRead); got != 3 {
		t.Errorf("CallRead attempts = %d, want 3", got)
	}
}

// ----- headers -----

func TestCommonHeadersAreSetOnce(t *testing.T) {
	var got http.Header
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		got = r.Header.Clone()
		_, _ = w.Write(okBody(t, map[string]string{"ok": "1"}))
	}))
	defer srv.Close()

	tr := newTestTransport(t, srv.URL, &stubTokens{token: "tok-1"}, nil)
	if err := tr.Do(context.Background(), Request{Op: "Bootstrap", Method: "GET", Path: "/client/bootstrap"}); err != nil {
		t.Fatalf("Do: %v", err)
	}

	for header, want := range map[string]string{
		"X-Client-Version":  "9.9.9",
		"X-Install-Id":      "install-abc",
		"X-Client-Platform": "windows",
		"X-Client-Arch":     "amd64",
		"Accept-Language":   "zh-CN",
		"Authorization":     "Bearer tok-1",
		"Accept":            "application/json",
	} {
		if got.Get(header) != want {
			t.Errorf("%s = %q, want %q", header, got.Get(header), want)
		}
	}
	if ct := got.Get("Content-Type"); ct != "" {
		t.Errorf("Content-Type = %q on a bodyless GET, want empty", ct)
	}
}

func TestPostBodiesAreJSON(t *testing.T) {
	var gotCT string
	var gotBody map[string]any
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotCT = r.Header.Get("Content-Type")
		_ = json.NewDecoder(r.Body).Decode(&gotBody)
		_, _ = w.Write(okBody(t, map[string]string{"ok": "1"}))
	}))
	defer srv.Close()

	tr := newTestTransport(t, srv.URL, nil, nil)
	if err := tr.Do(context.Background(), Request{
		Op: "SendCode", Method: "POST", Path: "/sms/send", Kind: CallWrite,
		Body: map[string]string{"phone": "13800000000"},
	}); err != nil {
		t.Fatalf("Do: %v", err)
	}
	if gotCT != "application/json" {
		t.Errorf("Content-Type = %q, want application/json", gotCT)
	}
	if gotBody["phone"] != "13800000000" {
		t.Errorf("body = %v", gotBody)
	}
}

func TestIdempotencyAndETagHeadersAreCallerControlled(t *testing.T) {
	var got http.Header
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		got = r.Header.Clone()
		_, _ = w.Write(okBody(t, map[string]string{"ok": "1"}))
	}))
	defer srv.Close()

	tr := newTestTransport(t, srv.URL, nil, nil)
	err := tr.Do(context.Background(), Request{
		Op: "Models", Method: "GET", Path: "/client/models",
		ClientRequestID: "crq-77", ETag: `W/"v3"`,
	})
	if err != nil {
		t.Fatalf("Do: %v", err)
	}
	if got.Get("Idempotency-Key") != "crq-77" {
		t.Errorf("Idempotency-Key = %q, want crq-77", got.Get("Idempotency-Key"))
	}
	if got.Get("If-None-Match") != `W/"v3"` {
		t.Errorf("If-None-Match = %q, want W/\"v3\"", got.Get("If-None-Match"))
	}
}

// TestNoAuthorizationWithoutASession: SendCode and Login run before there is a
// token, and sending a stale one would make the platform attribute the login to
// the previous session.
func TestNoAuthorizationWithoutASession(t *testing.T) {
	var sawAuth bool
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, sawAuth = r.Header["Authorization"]
		_, _ = w.Write(okBody(t, map[string]string{"ok": "1"}))
	}))
	defer srv.Close()

	tr := newTestTransport(t, srv.URL, nil, nil)
	if err := tr.Do(context.Background(), Request{Op: "SendCode", Method: "POST", Path: "/sms/send", Kind: CallWrite, Body: map[string]any{}}); err != nil {
		t.Fatalf("Do: %v", err)
	}
	if sawAuth {
		t.Fatal("Authorization was sent without a session")
	}
}

// ----- retry policy -----

// TestWriteIsNeverRetried is the SendCode rule: one press, one SMS.
func TestWriteIsNeverRetried(t *testing.T) {
	var calls atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		w.WriteHeader(http.StatusServiceUnavailable)
		_, _ = w.Write([]byte(`{"code":"upstream_unavailable","message":"down"}`))
	}))
	defer srv.Close()

	tr := newTestTransport(t, srv.URL, nil, noSleep)
	err := tr.Do(context.Background(), Request{Op: "SendCode", Method: "POST", Path: "/sms/send", Kind: CallWrite, Body: map[string]any{}})

	if got := calls.Load(); got != 1 {
		t.Fatalf("server saw %d requests, want 1 — a retried SendCode is a second SMS", got)
	}
	if CodeOf(err) != CodeUpstreamDown {
		t.Fatalf("code = %q, want %q", CodeOf(err), CodeUpstreamDown)
	}
}

func TestReadRetriesAreBoundedByTheAttemptTable(t *testing.T) {
	var calls atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		w.WriteHeader(http.StatusBadGateway)
		_, _ = w.Write([]byte(`{"code":"upstream_unavailable"}`))
	}))
	defer srv.Close()

	var slept []time.Duration
	tr := newTestTransport(t, srv.URL, nil, func(_ context.Context, d time.Duration) error {
		slept = append(slept, d)
		return nil
	})
	err := tr.Do(context.Background(), Request{Op: "Usage", Method: "GET", Path: "/usage", Kind: CallRead})

	if got := calls.Load(); got != int32(maxAttemptsRead) {
		t.Fatalf("server saw %d requests, want %d (the table's bound)", got, maxAttemptsRead)
	}
	if len(slept) != maxAttemptsRead-1 {
		t.Fatalf("slept %d times, want %d", len(slept), maxAttemptsRead-1)
	}
	// Backoff must grow, not repeat at a fixed interval.
	if slept[0] != backoffBase || slept[1] != backoffBase*2 {
		t.Fatalf("backoff = %v, want [%v %v]", slept, backoffBase, backoffBase*2)
	}
	if CodeOf(err) != CodeUpstreamDown {
		t.Fatalf("code = %q, want %q", CodeOf(err), CodeUpstreamDown)
	}
}

func TestRetryAfterIsHonoured(t *testing.T) {
	var calls atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if calls.Add(1) == 1 {
			w.WriteHeader(http.StatusTooManyRequests)
			_, _ = w.Write([]byte(`{"code":"rate_limited","retryAfterSec":2}`))
			return
		}
		_, _ = w.Write(okBody(t, map[string]string{"ok": "1"}))
	}))
	defer srv.Close()

	var slept []time.Duration
	tr := newTestTransport(t, srv.URL, nil, func(_ context.Context, d time.Duration) error {
		slept = append(slept, d)
		return nil
	})
	if err := tr.Do(context.Background(), Request{Op: "Usage", Method: "GET", Path: "/usage", Kind: CallRead}); err != nil {
		t.Fatalf("Do: %v", err)
	}
	if len(slept) != 1 || slept[0] != 2*time.Second {
		t.Fatalf("slept = %v, want exactly one 2s wait (the server's retryAfterSec)", slept)
	}
}

// TestLongRetryAfterIsReportedNotSleptOn: a platform answering "come back in an
// hour" is telling us to stop. Sleeping would hold the user's request open and
// still fail, which is indistinguishable from a hang. The cooldown must reach
// the caller instead.
func TestLongRetryAfterIsReportedNotSleptOn(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusTooManyRequests)
		_, _ = w.Write([]byte(`{"code":"rate_limited","retryAfterSec":3600}`))
	}))
	defer srv.Close()

	var slept []time.Duration
	tr := newTestTransport(t, srv.URL, nil, func(_ context.Context, d time.Duration) error {
		slept = append(slept, d)
		return nil
	})
	err := tr.Do(context.Background(), Request{Op: "Usage", Method: "GET", Path: "/usage", Kind: CallRead})

	if len(slept) != 0 {
		t.Fatalf("slept %v, want no sleep for a cooldown above %v", slept, backoffCeiling)
	}
	var pe *Error
	if !errors.As(err, &pe) {
		t.Fatalf("err = %T, want *Error", err)
	}
	if d, ok := pe.RetryAfter(); !ok || d != time.Hour {
		t.Fatalf("RetryAfter = %v, %v; the cooldown must be reportable to the caller", d, ok)
	}
}

// TestTotalBackoffIsCapped: a chain of long Retry-After headers must not pin a
// request open. With an 8s cooldown and a 15s budget only one wait fits, so the
// second attempt is refused rather than slept through.
func TestTotalBackoffIsCapped(t *testing.T) {
	var calls atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		w.WriteHeader(http.StatusServiceUnavailable)
		_, _ = w.Write([]byte(`{"code":"upstream_unavailable","retryAfterSec":8}`))
	}))
	defer srv.Close()

	var total time.Duration
	tr := newTestTransport(t, srv.URL, nil, func(_ context.Context, d time.Duration) error {
		total += d
		return nil
	})
	_ = tr.Do(context.Background(), Request{Op: "Usage", Method: "GET", Path: "/usage", Kind: CallRead})

	if total > maxTotalBackoff {
		t.Fatalf("total backoff = %v, want <= %v", total, maxTotalBackoff)
	}
	if got := calls.Load(); got != 2 {
		t.Fatalf("server saw %d requests; want 2 — the 15s budget admits one 8s wait, not two", got)
	}
	if total != 8*time.Second {
		t.Fatalf("total backoff = %v, want 8s", total)
	}
}

// TestSleepAbortsWithTheContext: a caller who gives up during a backoff must not
// have the request continue behind their back.
func TestSleepAbortsWithTheContext(t *testing.T) {
	var calls atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		w.WriteHeader(http.StatusServiceUnavailable)
		_, _ = w.Write([]byte(`{"code":"upstream_unavailable"}`))
	}))
	defer srv.Close()

	ctx, cancel := context.WithCancel(context.Background())
	tr := newTestTransport(t, srv.URL, nil, func(c context.Context, _ time.Duration) error {
		cancel()
		return c.Err()
	})
	_ = tr.Do(ctx, Request{Op: "Usage", Method: "GET", Path: "/usage", Kind: CallRead})

	if got := calls.Load(); got != 1 {
		t.Fatalf("server saw %d requests, want 1 — the caller cancelled during backoff", got)
	}
}

// ----- 401 refresh -----

// TestConcurrent401sCauseExactlyOneRefresh is the failure this whole design
// exists for. The platform rotates the refresh token on use, so N independent
// refreshes would invalidate all but one — logging the user out mid-page-load
// for no reason. The handler releases all N stale requests together so the
// coalescing does not depend on the scheduler's mood.
func TestConcurrent401sCauseExactlyOneRefresh(t *testing.T) {
	const callers = 8

	var requests atomic.Int32
	var stale atomic.Int32
	release := make(chan struct{})
	var once sync.Once

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests.Add(1)
		if r.Header.Get("Authorization") != "Bearer fresh" {
			if stale.Add(1) == callers {
				once.Do(func() { close(release) })
			}
			<-release
			w.WriteHeader(http.StatusUnauthorized)
			_, _ = w.Write([]byte(`{"code":"token_expired"}`))
			return
		}
		_, _ = w.Write(okBody(t, map[string]string{"ok": "1"}))
	}))
	defer srv.Close()

	tokens := &stubTokens{token: "stale", refreshDelay: 50 * time.Millisecond}
	tr := newTestTransport(t, srv.URL, tokens, noSleep)

	var wg sync.WaitGroup
	errs := make([]error, callers)
	start := make(chan struct{})
	for i := 0; i < callers; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			<-start
			errs[i] = tr.Do(context.Background(), Request{Op: "Usage", Method: "GET", Path: "/usage", Kind: CallRead})
		}(i)
	}
	close(start)
	wg.Wait()

	for i, err := range errs {
		if err != nil {
			t.Fatalf("caller %d: err = %v, want nil once the token was refreshed", i, err)
		}
	}
	refreshes, lost := tokens.counts()
	if refreshes != 1 {
		t.Fatalf("refreshes = %d, want exactly 1 — a rotating refresh token would be spent %d times", refreshes, refreshes)
	}
	if lost != 0 {
		t.Fatalf("AuthLost called %d times, want 0: every caller recovered", lost)
	}
	if got := requests.Load(); got != callers*2 {
		t.Fatalf("server saw %d requests, want %d (one 401 plus one replay each)", got, callers*2)
	}
}

// TestSecond401IsTerminal: a 401 *after* a successful refresh means the new
// token is also refused. Refreshing again would spend another rotation slot, and
// retrying would loop.
func TestSecond401IsTerminal(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
		_, _ = w.Write([]byte(`{"code":"token_expired"}`))
	}))
	defer srv.Close()

	tokens := &stubTokens{token: "stale"}
	tr := newTestTransport(t, srv.URL, tokens, noSleep)

	err := tr.Do(context.Background(), Request{Op: "Usage", Method: "GET", Path: "/usage", Kind: CallRead})

	if CodeOf(err) != CodeUnauthorized {
		t.Fatalf("code = %q, want %q", CodeOf(err), CodeUnauthorized)
	}
	refreshes, lost := tokens.counts()
	if refreshes != 1 {
		t.Fatalf("refreshes = %d, want 1 (a second would spend another rotation slot)", refreshes)
	}
	if lost != 1 {
		t.Fatalf("AuthLost called %d times, want 1", lost)
	}
}

// TestCallWriteCanRefresh: the retry loop runs once for a write, so if the 401
// replay lived in the loop a logged-in Logout could never recover its token.
func TestCallWriteCanRefresh(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer fresh" {
			w.WriteHeader(http.StatusUnauthorized)
			_, _ = w.Write([]byte(`{"code":"token_expired"}`))
			return
		}
		_, _ = w.Write(okBody(t, map[string]string{"ok": "1"}))
	}))
	defer srv.Close()

	tokens := &stubTokens{token: "stale"}
	tr := newTestTransport(t, srv.URL, tokens, noSleep)

	if err := tr.Do(context.Background(), Request{Op: "Logout", Method: "POST", Path: "/auth/logout", Kind: CallWrite, Body: map[string]any{}}); err != nil {
		t.Fatalf("Do: %v", err)
	}
	if refreshes, _ := tokens.counts(); refreshes != 1 {
		t.Fatalf("refreshes = %d, want 1", refreshes)
	}
}

// TestRefreshFailureReportsAuthLost: once the refresh is refused there is
// nothing left to try, and the caller needs to know to route the user to login.
func TestRefreshFailureReportsAuthLost(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
		_, _ = w.Write([]byte(`{"code":"token_expired"}`))
	}))
	defer srv.Close()

	boom := errors.New("refresh refused")
	tokens := &stubTokens{token: "stale", refreshErr: boom}
	tr := newTestTransport(t, srv.URL, tokens, noSleep)

	err := tr.Do(context.Background(), Request{Op: "Usage", Method: "GET", Path: "/usage", Kind: CallRead})
	if !errors.Is(err, boom) {
		t.Fatalf("err = %v, want it to wrap %v", err, boom)
	}
	if _, lost := tokens.counts(); lost != 1 {
		t.Fatalf("AuthLost calls = %d, want 1", lost)
	}
}

// ----- ETag / errors / network -----

func TestNotModifiedIsNotAnError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNotModified)
	}))
	defer srv.Close()

	tr := newTestTransport(t, srv.URL, nil, nil)
	var out map[string]string
	err := tr.Do(context.Background(), Request{Op: "Models", Method: "GET", Path: "/client/models", ETag: `"v1"`, Out: &out})

	if !errors.Is(err, ErrNotModified) {
		t.Fatalf("err = %v, want ErrNotModified so the caller reuses its cache", err)
	}
	if out != nil {
		t.Fatalf("out was written on a 304: %v", out)
	}
}

func TestServerCodesBecomeTypedErrors(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusForbidden)
		_, _ = w.Write([]byte(`{"code":"plan_expired","message":"expired","field":"plan","requestId":"rq-9"}`))
	}))
	defer srv.Close()

	tr := newTestTransport(t, srv.URL, nil, nil)
	err := tr.Do(context.Background(), Request{Op: "Usage", Method: "GET", Path: "/usage"})

	var pe *Error
	if !errors.As(err, &pe) {
		t.Fatalf("err = %T, want *Error", err)
	}
	if pe.Code != CodePlanExpired || pe.HTTPStatus != 403 || pe.Field != "plan" || pe.RequestID != "rq-9" || pe.Op != "Usage" {
		t.Fatalf("decoded error = %+v", pe)
	}
	if pe.Retryable() {
		t.Fatal("a 403 plan_expired was treated as retryable")
	}
}

// TestUnknownServerCodeIsPreserved: collapsing an unrecognised code into
// internal_error would hide the one case where a code was added without the
// client being told.
func TestUnknownServerCodeIsPreserved(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusConflict)
		_, _ = w.Write([]byte(`{"code":"brand_new_code"}`))
	}))
	defer srv.Close()

	tr := newTestTransport(t, srv.URL, nil, nil)
	err := tr.Do(context.Background(), Request{Op: "Usage", Method: "GET", Path: "/usage"})

	if got := CodeOf(err); got != Code("brand_new_code") {
		t.Fatalf("code = %q, want it preserved verbatim", got)
	}
}

// TestUnreachablePlatformIsItsOwnCode separates "we never got an answer" from
// "the platform said no". Conflating them would make a network outage look like
// an authorization failure, and the two have different recovery paths.
func TestUnreachablePlatformIsItsOwnCode(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {}))
	url := srv.URL
	srv.Close() // nothing is listening now

	tr := newTestTransport(t, url, nil, noSleep)
	err := tr.Do(context.Background(), Request{Op: "Usage", Method: "GET", Path: "/usage", Kind: CallRead})

	if CodeOf(err) != CodeNetworkUnavailable {
		t.Fatalf("code = %q, want %q", CodeOf(err), CodeNetworkUnavailable)
	}
}

// TestCallerCancellationKeepsItsCause: every user-initiated cancel would
// otherwise show up as a platform incident in the logs, and the caller could not
// tell "I cancelled" from "the network died".
func TestCallerCancellationKeepsItsCause(t *testing.T) {
	var calls atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		_, _ = w.Write(okBody(t, map[string]string{"ok": "1"}))
	}))
	defer srv.Close()

	tr := newTestTransport(t, srv.URL, nil, noSleep)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	err := tr.Do(ctx, Request{Op: "Usage", Method: "GET", Path: "/usage", Kind: CallRead})

	if !errors.Is(err, context.Canceled) {
		t.Fatalf("err = %v, want it to wrap context.Canceled so the cause is not lost", err)
	}
	if got := calls.Load(); got != 0 {
		t.Fatalf("server saw %d requests, want 0 — the caller cancelled first", got)
	}
}

// TestResponseShapeIsValidated: a 200 with no `data` must not decode into zero
// values, which would silently fabricate a state ("balance 0") the platform
// never asserted.
func TestResponseShapeIsValidated(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"requestId":"r1"}`))
	}))
	defer srv.Close()

	tr := newTestTransport(t, srv.URL, nil, nil)
	var out struct {
		Balance int `json:"balance"`
	}
	err := tr.Do(context.Background(), Request{Op: "Usage", Method: "GET", Path: "/usage", Out: &out})

	if err == nil {
		t.Fatalf("err = nil and out = %+v; an empty envelope must not decode to a zero balance", out)
	}
	if CodeOf(err) != CodeInternalError {
		t.Fatalf("code = %q, want %q", CodeOf(err), CodeInternalError)
	}
}

// TestBasePathIsPreserved guards the join: the profile's apiHost ends in /v1 and
// every Request.Path starts with /, so a naive concat would produce /v1//usage.
func TestBasePathIsPreserved(t *testing.T) {
	var seen string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		seen = r.URL.Path
		_, _ = w.Write(okBody(t, map[string]string{"ok": "1"}))
	}))
	defer srv.Close()

	tr := newTestTransport(t, srv.URL+"/v1", &stubTokens{}, nil)
	if err := tr.Do(context.Background(), Request{Op: "Usage", Method: "GET", Path: "/usage"}); err != nil {
		t.Fatalf("Do: %v", err)
	}
	if seen != "/v1/usage" {
		t.Fatalf("path = %q, want /v1/usage", seen)
	}
}

// TestQueryIsEncoded covers the ledger cursor/paging shape.
func TestQueryIsEncoded(t *testing.T) {
	var seen string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		seen = r.URL.RawQuery
		_, _ = w.Write(okBody(t, map[string]string{"ok": "1"}))
	}))
	defer srv.Close()

	tr := newTestTransport(t, srv.URL, nil, nil)
	err := tr.Do(context.Background(), Request{
		Op: "Ledger", Method: "GET", Path: "/ledger",
		Query: map[string][]string{"cursor": {"abc def"}, "limit": {"20"}},
	})
	if err != nil {
		t.Fatalf("Do: %v", err)
	}
	if seen != "cursor=abc+def&limit=20" {
		t.Fatalf("query = %q, want cursor=abc+def&limit=20", seen)
	}
}

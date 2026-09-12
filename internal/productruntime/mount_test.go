package productruntime_test

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/open-octo/octo-agent/internal/brand"
	"github.com/open-octo/octo-agent/internal/productclient"
	"github.com/open-octo/octo-agent/internal/productclient/clienttest"
	"github.com/open-octo/octo-agent/internal/productruntime"
	"github.com/open-octo/octo-agent/internal/server"
)

// This file drives the runtime through the REAL server, over a real loopback
// socket, mounted exactly the way cmd/octo-desktop mounts it. That distinction
// is the whole point:
//
//   - runtime_test.go calls rt.Handler() directly, which proves the handlers are
//     right. It cannot prove the UI can reach them.
//   - here the request travels the same road a browser's fetch takes: real mux,
//     real auth middleware, real socket.
//
// PR-2b1 is the cautionary tale (开发规范 §6.4.3): fifteen unit tests green and
// every logic branch correct, while the endpoints were still unreachable because
// nothing had mounted them. Unit tests prove the logic; this proves the road.
// L-A1a is only closed when both pass (E2E闭环清单.md §2.1).

// mountedHarness is a harness plus a real server serving on a loopback socket.
type mountedHarness struct {
	*harness
	baseURL string
}

// newMountedHarness reuses the unit-test harness for its platform stand-in and
// temp data root, then serves the same runtime through internal/server.
func newMountedHarness(t *testing.T) *mountedHarness {
	return newMountedHarnessWithToken(t, "")
}

// newMountedHarnessWithToken is the same road with the product gate armed, so a
// test can prove the window's full path — including the token it must present —
// works end to end. An empty token is the CLI shape (no gate).
func newMountedHarnessWithToken(t *testing.T, windowToken string) *mountedHarness {
	t.Helper()
	h := newHarness(t)
	return mountHarness(t, h, windowToken)
}

// newMountedHarnessWithControlPlane serves a runtime whose compile-time profile
// facts are chosen by the test, so the "unconfigured" and "no keys" blocked
// pages can be reached over the real road rather than only in a handler unit
// test. See newHarnessWithControlPlane for why the default is "configured".
func newMountedHarnessWithControlPlane(t *testing.T, status productruntime.ControlPlaneStatus) *mountedHarness {
	t.Helper()
	return mountHarness(t, newHarnessWithControlPlane(t, status), "")
}

func mountHarness(t *testing.T, h *harness, windowToken string) *mountedHarness {
	t.Helper()

	// The harness's own bare-handler server is redundant here; the point is to
	// exercise the mounted road, not the handler in isolation.
	h.local.Close()

	srv, err := server.New(server.Config{
		Addr:        "127.0.0.1:0",
		NoChannel:   true,
		NoMemory:    true,
		MountAPI:    h.rt.Mount,
		WindowToken: windowToken,
	})
	if err != nil {
		t.Fatalf("server.New: %v", err)
	}

	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	go func() { _ = srv.ServeOn(ln) }()
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_ = srv.Shutdown(ctx)
	})

	return &mountedHarness{harness: h, baseURL: "http://" + ln.Addr().String()}
}

// request issues a request over the real socket (not the harness's bare handler).
func (m *mountedHarness) request(t *testing.T, method, path string, body any, headers map[string]string) (int, []byte) {
	t.Helper()

	var reader io.Reader
	if body != nil {
		raw, err := json.Marshal(body)
		if err != nil {
			t.Fatalf("marshal body: %v", err)
		}
		reader = bytes.NewReader(raw)
	}
	req, err := http.NewRequest(method, m.baseURL+path, reader)
	if err != nil {
		t.Fatalf("new request: %v", err)
	}
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	for k, v := range headers {
		req.Header.Set(k, v)
	}

	resp, err := (&http.Client{Timeout: 10 * time.Second}).Do(req)
	if err != nil {
		t.Fatalf("%s %s: %v", method, path, err)
	}
	defer resp.Body.Close()
	raw, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatalf("read body: %v", err)
	}
	return resp.StatusCode, raw
}

// TestMountedProductRoutesRequireTheWindowToken pins L-B1b's request gate on the
// road the window actually takes. With a token configured, /api/product/state —
// the UI's very first call — is refused until the window presents its token,
// because the fork's routes are registered through the server's window-gated
// registrar and nothing else is. An unauthenticated window must not read state,
// or the interface gate would be decided by a request the gate never saw.
func TestMountedProductRoutesRequireTheWindowToken(t *testing.T) {
	// The wire name is spelled out rather than imported: it is a Go/JS boundary
	// string (web/src/lib/product.ts:21), and a test that shares the constant
	// with the implementation cannot catch a rename that breaks the browser.
	const header = "X-Octo-Window-Token"
	const token = "3f2a1b0c9d8e7f605142332415061728293a3b3c4d4e4f505152535455565758"

	m := newMountedHarnessWithToken(t, token)

	status, raw := m.request(t, http.MethodGet, "/api/product/state", nil, nil)
	if status != http.StatusForbidden {
		t.Fatalf("GET /api/product/state without a token = %d, want 403 (body: %.200s)", status, raw)
	}

	status, raw = m.request(t, http.MethodGet, "/api/product/state", nil, map[string]string{header: token})
	if status != http.StatusOK {
		t.Fatalf("GET /api/product/state with the token = %d, want 200 (body: %.200s)", status, raw)
	}
	var state map[string]any
	if err := json.Unmarshal(raw, &state); err != nil {
		t.Fatalf("state is not JSON (%v): %.200s", err, raw)
	}
	if _, ok := state["loggedIn"]; !ok {
		t.Fatalf("the gated call returned a non-state body: %.200s", raw)
	}
}

// TestMountedStateIsReachableThroughTheRealServer is the smallest end-to-end
// assertion: the UI's very first call reaches the runtime through the server
// that actually serves it. Before the mount seam existed, this path fell
// through to the static-file handler instead of the product runtime.
func TestMountedStateIsReachableThroughTheRealServer(t *testing.T) {
	m := newMountedHarness(t)

	status, raw := m.request(t, http.MethodGet, "/api/product/state", nil, nil)
	if status != http.StatusOK {
		t.Fatalf("GET /api/product/state = %d, want 200 (body: %.200s)", status, raw)
	}

	// Assert the shape, not just the status: a 200 that is really the SPA's
	// index.html would otherwise pass. The contract returns this one endpoint
	// unwrapped, unlike login's {"state": ...} (本地API契约.md §2.1).
	var state map[string]any
	if err := json.Unmarshal(raw, &state); err != nil {
		t.Fatalf("state is not JSON (%v): %.200s", err, raw)
	}
	// loggedIn is part of the contract's ProductStateDTO - it is the field the
	// frontend reads to decide which page to show. installId is deliberately NOT
	// here: it identifies the installation in request headers, and the UI never
	// renders it (需求基线 E6.1).
	if _, ok := state["loggedIn"]; !ok {
		t.Errorf("state has no loggedIn - got keys %v", sortedKeys(state))
	}
	if _, nested := state["state"]; nested {
		t.Error(`state must be unwrapped, but the body nests it under "state"`)
	}
}

// TestMountedRouteGoesThroughRequireAuth pins the design decision behind the
// seam's shape. The mount hands the runtime the server's own authenticated
// registrar rather than the raw mux, so every product route inherits requireAuth
// (and its no-store stamping) for free.
//
// A relayed request is the cheap way to observe that: requireAuth rejects a
// request carrying forwarding headers, because otherwise exposing the port
// through a proxy would hand out the loopback exemption (see isForwarded). Had
// the route been hung on the raw mux, this request would sail through - so this
// test fails if anyone ever "simplifies" the seam into a raw mux hook.
func TestMountedRouteGoesThroughRequireAuth(t *testing.T) {
	m := newMountedHarness(t)

	status, raw := m.request(t, http.MethodGet, "/api/product/state", nil, map[string]string{
		"X-Forwarded-For": "203.0.113.7",
	})
	if status != http.StatusUnauthorized {
		t.Fatalf("relayed GET /api/product/state = %d, want 401 - the mount must not bypass requireAuth (body: %.200s)", status, raw)
	}
}

// TestFirstActivationEndToEnd is L-A1a: five fields in, main interface out.
//
// It asserts the observable contract rather than the plumbing: the request
// succeeds through the real server, the response carries the state the UI
// renders, and both files on disk end up in the shape 需求基线 E6.1 / E6.3
// describe.
func TestFirstActivationEndToEnd(t *testing.T) {
	m := newMountedHarness(t)

	if status, raw := m.request(t, http.MethodPost, "/api/product/send-code", map[string]any{
		"phone": "13800001234",
	}, nil); status != http.StatusOK {
		t.Fatalf("send-code = %d, want 200 (body: %.200s)", status, raw)
	}

	status, raw := m.request(t, http.MethodPost, "/api/product/login", map[string]any{
		"phone":          "13800001234",
		"code":           clienttest.FixtureSMSCode,
		"activationCode": clienttest.FixtureActivationCode,
		"boxCode":        clienttest.FixtureBoxCode,
		"nickname":       "tester",
	}, nil)
	if status != http.StatusOK {
		t.Fatalf("login = %d, want 200 (body: %.200s)", status, raw)
	}

	var resp struct {
		State struct {
			LoggedIn   bool `json:"loggedIn"`
			Activated  bool `json:"activated"`
			Activation *struct {
				BoxCode string `json:"boxCode"`
			} `json:"activation"`
		} `json:"state"`
	}
	if err := json.Unmarshal(raw, &resp); err != nil {
		t.Fatalf("login response is not JSON (%v): %.200s", err, raw)
	}
	if !resp.State.LoggedIn || !resp.State.Activated {
		t.Errorf("after activation loggedIn=%v activated=%v, want both true", resp.State.LoggedIn, resp.State.Activated)
	}

	// The box code the UI shows is the platform's record, not the string the
	// user typed. They coincide in the happy path, so assert against the
	// stand-in's value to keep the source of truth explicit (E1 rule 6).
	if resp.State.Activation == nil {
		t.Fatal("activation is nil after a successful activation")
	}
	if resp.State.Activation.BoxCode != clienttest.FixtureBoxCode {
		t.Errorf("activation.boxCode = %q, want %q (from the platform)", resp.State.Activation.BoxCode, clienttest.FixtureBoxCode)
	}

	// The state file is the durable half of the loop.
	stateRaw := readFile(t, filepath.Join(m.root, "product-state.json"))
	var onDisk struct {
		InstallID  string `json:"installId"`
		LoggedIn   bool   `json:"loggedIn"`
		Activated  bool   `json:"activated"`
		Activation *struct {
			BoxCode string `json:"boxCode"`
		} `json:"activation"`
	}
	if err := json.Unmarshal([]byte(stateRaw), &onDisk); err != nil {
		t.Fatalf("state file is not JSON (%v)", err)
	}
	if onDisk.InstallID == "" {
		t.Error("state file has no installId")
	}
	if !onDisk.LoggedIn || !onDisk.Activated {
		t.Errorf("state file loggedIn=%v activated=%v, want both true", onDisk.LoggedIn, onDisk.Activated)
	}
	if onDisk.Activation == nil || onDisk.Activation.BoxCode != clienttest.FixtureBoxCode {
		t.Errorf("state file activation = %+v, want boxCode %q", onDisk.Activation, clienttest.FixtureBoxCode)
	}

	// No access token anywhere in the state file (E6.3): the durable state is
	// non-sensitive by construction, so a diagnostic bundle may carry it.
	for _, needle := range []string{"accessToken", "refreshToken", "access_token", "refresh_token"} {
		if strings.Contains(strings.ToLower(stateRaw), strings.ToLower(needle)) {
			t.Errorf("product-state.json contains %q - durable product state must never hold a token", needle)
		}
	}

	// The refresh token does live in credential.json - that is what makes the
	// drive portable (E6 rule 3, L-E2).
	credRaw := readFile(t, filepath.Join(m.root, "credential.json"))
	var cred struct {
		RefreshToken string `json:"refreshToken"`
	}
	if err := json.Unmarshal([]byte(credRaw), &cred); err != nil {
		t.Fatalf("credential file is not JSON (%v)", err)
	}
	if cred.RefreshToken == "" {
		t.Error("credential.json has no refreshToken")
	}
}

// TestLocallyRejectedLoginDoesNotConsumeTheActivationCode is L-A1a's exception
// branch, and it is the one that actually protects the user.
//
// The form is filled in with valid credentials but an empty nickname. Local
// validation must reject it before the platform is asked, because a
// single-use activation code (E1 rule 2) that the platform consumes on a
// request the user never saw acknowledged would strand them - they would get
// back "this code has already been used" with nothing to show for it.
//
// The discriminating step is the second call: it can only succeed if the first
// one never reached the platform. That is why this asserts on a later request
// rather than on a call count - the stand-in has no way to report "a login was
// refused locally", and a count would not have distinguished the two cases.
func TestLocallyRejectedLoginDoesNotConsumeTheActivationCode(t *testing.T) {
	m := newMountedHarness(t)

	if status, raw := m.request(t, http.MethodPost, "/api/product/send-code", map[string]any{
		"phone": "13800001234",
	}, nil); status != http.StatusOK {
		t.Fatalf("send-code = %d, want 200 (body: %.200s)", status, raw)
	}

	fields := map[string]any{
		"phone":          "13800001234",
		"code":           clienttest.FixtureSMSCode,
		"activationCode": clienttest.FixtureActivationCode,
		"boxCode":        clienttest.FixtureBoxCode,
		"nickname":       "",
	}

	status, raw := m.request(t, http.MethodPost, "/api/product/login", fields, nil)
	if status != http.StatusBadRequest {
		t.Fatalf("login with an empty nickname = %d, want 400 (body: %.200s)", status, raw)
	}
	var resp struct {
		FieldErrors map[string]string `json:"fieldErrors"`
	}
	if err := json.Unmarshal(raw, &resp); err != nil {
		t.Fatalf("error body is not JSON (%v): %.200s", err, raw)
	}
	if _, ok := resp.FieldErrors["nickname"]; !ok {
		t.Errorf("want a field error on nickname, got %v", resp.FieldErrors)
	}

	// Now fix only the nickname. This fails with activation_code_used if the
	// rejected attempt above leaked through to the platform.
	fields["nickname"] = "tester"
	status, raw = m.request(t, http.MethodPost, "/api/product/login", fields, nil)
	if status != http.StatusOK {
		t.Fatalf("login after fixing the nickname = %d, want 200 - the rejected attempt must not have consumed the single-use code (body: %.200s)", status, raw)
	}
}

func readFile(t *testing.T, path string) string {
	t.Helper()
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}
	return string(raw)
}

func sortedKeys(m map[string]any) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

// ---------------------------------------------------------------------------
// PR-4b: the catalog, and the three end-to-end loops V-29 said were missing.
//
// Everything here travels the road the window's fetch takes: real mux, real
// auth middleware, real socket. The catalog half has to, because the failure
// mode PR-4b can produce is not "the wrong boolean" but "nothing ever fetched
// it" - and a handler unit test cannot see that.
// ---------------------------------------------------------------------------

// post issues a JSON request over the real socket and decodes the body.
func (m *mountedHarness) post(t *testing.T, path string, body any) (int, map[string]any) {
	t.Helper()
	status, raw := m.request(t, http.MethodPost, path, body, nil)
	var decoded map[string]any
	if len(raw) > 0 {
		if err := json.Unmarshal(raw, &decoded); err != nil {
			t.Fatalf("decode %s (%d): %v (body: %.200s)", path, status, err, raw)
		}
	}
	return status, decoded
}

// firstActivation runs the five-field first activation through the mounted road
// and fails if it did not succeed.
func (m *mountedHarness) firstActivation(t *testing.T) {
	t.Helper()
	if status, body := m.post(t, "/api/product/send-code", map[string]any{"phone": "13800001234"}); status != http.StatusOK {
		t.Fatalf("send-code = %d (%v), want 200", status, body)
	}
	status, body := m.post(t, "/api/product/login", map[string]any{
		"phone":          "13800001234",
		"code":           clienttest.FixtureSMSCode,
		"nickname":       "tester",
		"activationCode": clienttest.FixtureActivationCode,
		"boxCode":        clienttest.FixtureBoxCode,
	})
	if status != http.StatusOK {
		t.Fatalf("first activation = %d (%v), want 200", status, body)
	}
}

// laterLogin is the second login (需求基线 E2): the account is already
// activated, so no activation credential is sent - but phone, code AND nickname
// still are. Nickname is not optional on any login: the second-login form
// prefills the last one and 本地API契约 §2.3 lists it as a request field, so
// omitting it is a field-level error, not a mode switch.
func (m *mountedHarness) laterLogin(t *testing.T, phone string) (int, map[string]any) {
	t.Helper()
	if status, body := m.post(t, "/api/product/send-code", map[string]any{"phone": phone}); status != http.StatusOK {
		t.Fatalf("send-code = %d (%v), want 200", status, body)
	}
	return m.post(t, "/api/product/login", map[string]any{
		"phone":    phone,
		"code":     clienttest.FixtureSMSCode,
		"nickname": "tester",
	})
}

// cachedCatalog is the on-disk shape 需求基线 B2 规则 1 describes, parsed the way
// a reader with no network would parse it.
type cachedCatalog struct {
	SchemaVersion  int                          `json:"schemaVersion"`
	CatalogVersion string                       `json:"catalogVersion"`
	FetchedAt      string                       `json:"fetchedAt"`
	ExpiresAt      string                       `json:"expiresAt"`
	KeyID          string                       `json:"keyId"`
	Audience       string                       `json:"audience"`
	Envelope       productclient.PolicyEnvelope `json:"envelope"`
}

func (m *mountedHarness) cachedCatalog(t *testing.T) cachedCatalog {
	t.Helper()
	raw := readFile(t, filepath.Join(m.root, "catalog.json"))
	var cached cachedCatalog
	if err := json.Unmarshal([]byte(raw), &cached); err != nil {
		t.Fatalf("catalog.json is not JSON (%v): %.300s", err, raw)
	}
	return cached
}

func (m *mountedHarness) hasCatalog() bool {
	_, err := os.Stat(filepath.Join(m.root, "catalog.json"))
	return err == nil
}

// TestCatalogIsCachedAndVerifiesOfflineAfterLogin is L-C1a's judgement: after a
// login, the signed catalog is on the drive, and it can be verified there
// without asking anybody.
//
// "Offline" is asserted, not assumed: the platform's call count is sampled
// around the verification, so a future implementation that quietly refetches
// instead of trusting the file fails here. That property is the whole reason
// B2 says to keep the original envelope - a cache that needs the network to be
// read is not a cache.
func TestCatalogIsCachedAndVerifiesOfflineAfterLogin(t *testing.T) {
	m := newMountedHarness(t)
	m.firstActivation(t)

	cached := m.cachedCatalog(t)
	if cached.SchemaVersion != 1 {
		t.Errorf("schemaVersion = %d, want 1", cached.SchemaVersion)
	}
	if cached.CatalogVersion != clienttest.FixturePolicyVersion {
		t.Errorf("catalogVersion = %q, want the platform's %q", cached.CatalogVersion, clienttest.FixturePolicyVersion)
	}
	if cached.KeyID != clienttest.FixtureSigningKeyID {
		t.Errorf("keyId = %q, want %q", cached.KeyID, clienttest.FixtureSigningKeyID)
	}
	// The audience is the value this build answers to, read from its single
	// owner - not a literal repeated here, which is how a rebrand silently makes
	// every catalog unverifiable.
	if cached.Audience != brand.Load().BrandID {
		t.Errorf("audience = %q, want the build's brandId %q", cached.Audience, brand.Load().BrandID)
	}
	expires, err := time.Parse(time.RFC3339, cached.ExpiresAt)
	if err != nil {
		t.Fatalf("expiresAt %q: %v", cached.ExpiresAt, err)
	}
	if !expires.After(time.Now()) {
		t.Errorf("expiresAt = %s is already in the past", cached.ExpiresAt)
	}

	before := m.platform.BootstrapCount()
	policy, err := cached.Envelope.Verify(productclient.VerifyOptions{
		TrustedKeys: map[string]string{clienttest.FixtureSigningKeyID: clienttest.FixtureSigningPublicKey()},
		Audience:    brand.Load().BrandID,
		Now:         time.Now(),
		Skew:        time.Minute,
	})
	if err != nil {
		t.Fatalf("the cached catalog does not verify: %v", err)
	}
	if len(policy.Catalog.Models) == 0 {
		t.Error("the cached catalog has no models; there is nothing for the picker to show")
	}
	if after := m.platform.BootstrapCount(); after != before {
		t.Errorf("verifying the cache cost %d extra platform call(s); it must be readable offline", after-before)
	}
}

// TestACatalogFailureDoesNotBlockTheLogin is 需求基线 B1 规则 1's shape: a
// catalog that cannot be fetched leaves the picker to PR-4c's wording, but the
// user is signed in either way. Failing the login instead would lock a paying
// customer out over a catalog they are not using yet.
func TestACatalogFailureDoesNotBlockTheLogin(t *testing.T) {
	cases := map[string]func(m *mountedHarness){
		"the catalog cannot be fetched": func(m *mountedHarness) {
			m.platform.FailBootstrap(502, productclient.CodeUpstreamUnavailable)
		},
		"the signature does not cover what arrived": func(m *mountedHarness) {
			m.platform.TamperPolicy()
		},
	}
	for name, inject := range cases {
		t.Run(name, func(t *testing.T) {
			m := newMountedHarness(t)
			inject(m)
			m.firstActivation(t)

			if m.hasCatalog() {
				t.Error("a catalog was cached from a fetch that did not produce a usable one")
			}
			state := m.readStateFile()
			if loggedIn, _ := state["loggedIn"].(bool); !loggedIn {
				t.Errorf("the user was not signed in (%v); a missing catalog is not a failed login", state)
			}
			// The trade-off is visible in the return value, not swallowed: this
			// PR logs the reason and PR-4c turns it into the three degradation
			// behaviours and their wording (需求基线 B4).
		})
	}
}

// TestARolledBackCatalogDoesNotReplaceTheCache walks the downgrade defence over
// the real road. A validly signed but older catalog is exactly what an attacker
// who can answer for the platform would replay, and the cache - not the
// verifier - is where that has to be caught (开发计划 PR-4b 缺口 ③).
func TestARolledBackCatalogDoesNotReplaceTheCache(t *testing.T) {
	m := newMountedHarness(t)
	m.firstActivation(t)
	before := readFile(t, filepath.Join(m.root, "catalog.json"))

	m.platform.SetCatalogVersion("2026-09-01.0")
	if status, body := m.laterLogin(t, "13800001234"); status != http.StatusOK {
		t.Fatalf("second login = %d (%v), want 200 - a rolled-back catalog is not a login failure", status, body)
	}

	if after := readFile(t, filepath.Join(m.root, "catalog.json")); after != before {
		t.Error("the cache was replaced by an older catalog")
	}
}

// TestSessionExpiryIsReachableThroughBootstrap is L-A6 end to end, and it is
// the first test that can exist: bootstrap is the first authorised platform call
// any production code makes, so before PR-4b the "refresh refused -> clear the
// credential -> back to the blocked page" path had no way to run (V-29).
//
// The fault is a platform that hands out tokens it will not honour, which is
// what a revoked session looks like from here: the login succeeds, and the very
// first authorised call is refused, with the refresh refused too. The activation
// code is consumed by the platform before it refuses - that is inherent to the
// fault, and not something the client can undo.
func TestSessionExpiryIsReachableThroughBootstrap(t *testing.T) {
	m := newMountedHarness(t)
	m.platform.RefuseSessionsAfterLogin()

	if status, body := m.post(t, "/api/product/send-code", map[string]any{"phone": "13800001234"}); status != http.StatusOK {
		t.Fatalf("send-code = %d (%v), want 200", status, body)
	}
	status, body := m.post(t, "/api/product/login", map[string]any{
		"phone":          "13800001234",
		"code":           clienttest.FixtureSMSCode,
		"nickname":       "tester",
		"activationCode": clienttest.FixtureActivationCode,
		"boxCode":        clienttest.FixtureBoxCode,
	})
	if status != http.StatusUnauthorized {
		t.Fatalf("login = %d (%v), want 401: the session the platform issues is already refused", status, body)
	}
	if got := codeOf(t, body); got != productclient.CodeUnauthorized {
		t.Errorf("code = %q, want %q", got, productclient.CodeUnauthorized)
	}

	if m.credentialFileExists() {
		if credRaw := readFile(t, filepath.Join(m.root, "credential.json")); strings.Contains(credRaw, "refreshToken") {
			t.Error("the refusal left a refresh token on disk; the next startup would read it as a live session")
		}
	}
	state := m.readStateFile()
	if loggedIn, _ := state["loggedIn"].(bool); loggedIn {
		t.Error("loggedIn survived a refused session")
	}
	// E7: an expired session is not an un-activation. The activation record and
	// the bound number stay, which is what makes the second login ask for only
	// phone and code - and matters here more than anywhere, because the
	// activation code has just been consumed.
	if activated, _ := state["activated"].(bool); !activated {
		t.Errorf("activated was cleared by a refused session: %v", state)
	}
	if m.hasCatalog() {
		t.Error("a catalog was cached for a session the platform had already refused")
	}
}

// TestSecondLoginEndToEnd is L-A2 over the real road: an installation that has
// already activated signs in again with phone and code alone, and the box code
// it never typed this time still reaches the license page.
func TestSecondLoginEndToEnd(t *testing.T) {
	m := newMountedHarness(t)
	m.firstActivation(t)

	status, body := m.laterLogin(t, "13800001234")
	if status != http.StatusOK {
		t.Fatalf("second login = %d (%v), want 200", status, body)
	}
	state, ok := body["state"].(map[string]any)
	if !ok {
		t.Fatalf("login body has no state object: %v", body)
	}
	activation, _ := state["activation"].(map[string]any)
	if activation == nil || activation["boxCode"] != clienttest.FixtureBoxCode {
		t.Errorf("activation = %v, want the platform's box code %q", activation, clienttest.FixtureBoxCode)
	}
}

// TestLogoutThenReloginEndToEnd is L-A3, and it carries a specific scar: the
// logout path once cleared the activation flag, so the blocked page came back
// asking for the U-disk activation code - which can be used exactly once, so a
// single click on 登出 locked the user out for good (V-22).
//
// The assertion that catches a regression is the last one: signing in again with
// phone and code only must succeed. If the flag were cleared again, this login
// would be a first activation and the platform would refuse it.
func TestLogoutThenReloginEndToEnd(t *testing.T) {
	m := newMountedHarness(t)
	m.firstActivation(t)
	if !m.credentialFileExists() {
		t.Fatal("no credential after activation; the logout assertions below would pass vacuously")
	}

	if status, body := m.post(t, "/api/product/logout", nil); status != http.StatusOK {
		t.Fatalf("logout = %d (%v), want 200", status, body)
	}
	state := m.readStateFile()
	if loggedIn, _ := state["loggedIn"].(bool); loggedIn {
		t.Error("loggedIn survived a logout")
	}
	if activated, _ := state["activated"].(bool); !activated {
		t.Error("logout cleared the activation flag; E7 says logging out is not un-activating")
	}
	if m.credentialFileExists() {
		t.Error("the credential file survived a logout; deleting it is what ends the session")
	}

	status, body := m.laterLogin(t, "13800001234")
	if status != http.StatusOK {
		t.Fatalf("login after logout = %d (%v), want 200 - the form must not be asking for a used-up activation code", status, body)
	}
	state, _ = body["state"].(map[string]any)
	if loggedIn, _ := state["loggedIn"].(bool); !loggedIn {
		t.Errorf("loggedIn = false after signing back in: %v", state)
	}
}

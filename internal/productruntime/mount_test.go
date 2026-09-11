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

	"github.com/open-octo/octo-agent/internal/productclient/clienttest"
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

package productruntime_test

import (
	"encoding/json"
	"io"
	"net/http"
	"testing"

	"github.com/open-octo/octo-agent/internal/productruntime"
)

// L-B2 says an unconfigured build must say so BEFORE the user types, rather than
// after a failed round trip. The blocked page therefore needs the two facts the
// profile already knows, over the wire, unauthenticated.
//
// These run through the real server (mount_test.go's road), because the failure
// this guards against is not a wrong boolean - it is a route nobody mounted, in
// which case the frontend gets a 404 and falls back to the login form forever.
// That is the PR-2b1 lesson (开发规范 §6.4.3): logic correct, road missing.

// controlPlaneDTO mirrors 本地API契约 §2.13. Both fields are asserted present in
// every case: the frontend tells the four blocked-page outcomes apart by their
// values, so a `false` that went missing would silently merge two pages.
type controlPlaneDTO struct {
	Configured     *bool `json:"configured"`
	HasTrustedKeys *bool `json:"hasTrustedKeys"`
}

func readControlPlane(t *testing.T, h *mountedHarness) (int, controlPlaneDTO) {
	t.Helper()
	// No session is established anywhere in this file, and none is needed: the
	// whole point of the endpoint is that it answers while the user is still on
	// the login page (本地API契约 §2.13 "需登录：否").
	res, err := http.Get(h.baseURL + "/api/product/control-plane")
	if err != nil {
		t.Fatalf("GET control-plane: %v", err)
	}
	defer res.Body.Close()
	raw, err := io.ReadAll(res.Body)
	if err != nil {
		t.Fatalf("read body: %v", err)
	}
	var dto controlPlaneDTO
	if res.StatusCode == http.StatusOK {
		if err := json.Unmarshal(raw, &dto); err != nil {
			t.Fatalf("decode %s: %v", raw, err)
		}
	}
	return res.StatusCode, dto
}

// TestControlPlaneEndpointIsReachable is the anti-regression nail for the road
// itself. A 404 here would be invisible in unit tests and would strand every
// unconfigured build on the login form with no explanation.
func TestControlPlaneEndpointIsReachable(t *testing.T) {
	h := newMountedHarness(t)

	status, _ := readControlPlane(t, h)
	if status != http.StatusOK {
		t.Fatalf("status = %d, want 200: the route must be mounted (本地API契约 §2.13)", status)
	}
}

// TestControlPlaneReportsAnUnconfiguredBuild is L-B2's first page: no control
// plane in this build. Both facts are false, and the frontend renders the
// "unconfigured" page - it must NOT read this as a network outage.
func TestControlPlaneReportsAnUnconfiguredBuild(t *testing.T) {
	h := newMountedHarnessWithControlPlane(t, productruntime.ControlPlaneStatus{})

	status, dto := readControlPlane(t, h)
	if status != http.StatusOK {
		t.Fatalf("status = %d, want 200", status)
	}
	if dto.Configured == nil || dto.HasTrustedKeys == nil {
		t.Fatalf("both fields must be present, got %+v", dto)
	}
	if *dto.Configured || *dto.HasTrustedKeys {
		t.Errorf("got configured=%v hasTrustedKeys=%v, want false/false for a build with no control plane",
			*dto.Configured, *dto.HasTrustedKeys)
	}
}

// TestControlPlaneReportsMissingTrustedKeys is L-B2's second page, and the one
// the old code could not reach at all: HasTrustedKeys() had zero call sites, so
// "has an address but trusts no key" had no signal that could produce it.
//
// It is a DIFFERENT page from the unconfigured one, and the difference is the
// point: this build can be logged into, it just can never verify a catalog, so
// telling the user "not configured" would send them looking for a network
// problem instead of a bad package.
func TestControlPlaneReportsMissingTrustedKeys(t *testing.T) {
	h := newMountedHarnessWithControlPlane(t, productruntime.ControlPlaneStatus{
		Configured:     true,
		HasTrustedKeys: false,
	})

	status, dto := readControlPlane(t, h)
	if status != http.StatusOK {
		t.Fatalf("status = %d, want 200", status)
	}
	if !*dto.Configured {
		t.Error("configured = false, want true: this build names a host")
	}
	if *dto.HasTrustedKeys {
		t.Error("hasTrustedKeys = true, want false: no key is trusted, so no catalog can verify")
	}
}

// TestControlPlaneReportsAHealthyBuild pins the fourth blocked-page outcome: a
// normal build reports both true, and the frontend consequently shows the login
// form rather than either misconfiguration page.
func TestControlPlaneReportsAHealthyBuild(t *testing.T) {
	h := newMountedHarness(t) // the harness default is a configured, key-trusting build

	status, dto := readControlPlane(t, h)
	if status != http.StatusOK {
		t.Fatalf("status = %d, want 200", status)
	}
	if !*dto.Configured || !*dto.HasTrustedKeys {
		t.Errorf("got configured=%v hasTrustedKeys=%v, want true/true for a normal build",
			*dto.Configured, *dto.HasTrustedKeys)
	}
}

// TestControlPlaneWorksBehindTheWindowGate is the road the desktop build
// actually takes. The endpoint must stay reachable when the gate is armed -
// which it is in the window - while still being an unauthenticated call in the
// sense that matters here: no session, no login, no credential.
func TestControlPlaneWorksBehindTheWindowGate(t *testing.T) {
	const token = "window-token-for-control-plane"
	h := newMountedHarnessWithToken(t, token)

	req, err := http.NewRequest(http.MethodGet, h.baseURL+"/api/product/control-plane", nil)
	if err != nil {
		t.Fatalf("new request: %v", err)
	}
	req.Header.Set("X-Octo-Window-Token", token)
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("GET control-plane: %v", err)
	}
	defer res.Body.Close()

	if res.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want 200: the window must be able to read this before login", res.StatusCode)
	}
	var dto controlPlaneDTO
	if err := json.NewDecoder(res.Body).Decode(&dto); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if dto.Configured == nil || dto.HasTrustedKeys == nil {
		t.Errorf("both fields must be present, got %+v", dto)
	}
}

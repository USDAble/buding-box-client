package productgate

import (
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"

	"github.com/open-octo/octo-agent/internal/productstate"
)

func newStore(t *testing.T, loggedIn bool) *productstate.Store {
	t.Helper()
	s, err := productstate.Open(filepath.Join(t.TempDir(), "product-state.json"))
	if err != nil {
		t.Fatalf("Open = %v", err)
	}
	if loggedIn {
		if err := s.Mutate(func(st *productstate.State) error {
			st.Account = &productstate.Account{Phone: "13800001234"}
			return nil
		}); err != nil {
			t.Fatalf("Mutate = %v", err)
		}
	}
	return s
}

func reqWithToken(token string) *http.Request {
	r := httptest.NewRequest(http.MethodGet, "/api/chat", nil)
	if token != "" {
		r.Header.Set(HeaderWindowToken, token)
	}
	return r
}

func TestAllowCombinations(t *testing.T) {
	const tok = "abc123"
	cases := []struct {
		name      string
		gateToken string
		presented string
		loggedIn  bool
		wantOK    bool
		wantReas  string
	}{
		{"no window (serve): always pass", "", "xyz", false, true, ""},
		{"no token presented: pass (CLI)", tok, "", false, true, ""},
		{"match + logged in: pass", tok, tok, true, true, ""},
		{"match + not logged in: 403", tok, tok, false, false, "not_logged_in"},
		{"mismatch: 403 bad_token", tok, "wrong", true, false, "bad_token"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			g := New(tc.gateToken, newStore(t, tc.loggedIn))
			ok, reason := g.Allow(reqWithToken(tc.presented))
			if ok != tc.wantOK || reason != tc.wantReas {
				t.Fatalf("Allow = (%v, %q), want (%v, %q)", ok, reason, tc.wantOK, tc.wantReas)
			}
		})
	}
}

// TestAllowWebSocketQuery pins the /ws contract: the browser WebSocket API
// cannot set custom headers, so the token rides a query parameter instead.
func TestAllowWebSocketQuery(t *testing.T) {
	const tok = "abc123"
	g := New(tok, newStore(t, true))

	r := httptest.NewRequest(http.MethodGet, "/ws?window_token="+tok, nil)
	if ok, _ := g.Allow(r); !ok {
		t.Fatal("a matching query token on /ws must pass")
	}

	r = httptest.NewRequest(http.MethodGet, "/ws?window_token=wrong", nil)
	if ok, reason := g.Allow(r); ok || reason != "bad_token" {
		t.Fatalf("a wrong query token on /ws must 403, got (%v, %q)", ok, reason)
	}

	// A token on a non-/ws route must NOT be read from the query string.
	r = httptest.NewRequest(http.MethodGet, "/api/chat?window_token="+tok, nil)
	if ok, _ := g.Allow(r); !ok {
		t.Fatal("a query token on a non-/ws route is ignored → passes as not-the-window")
	}
}

func TestMiddlewareDeniesWithBody(t *testing.T) {
	g := New("abc123", newStore(t, false))
	var served bool
	h := g.Middleware(http.HandlerFunc(func(http.ResponseWriter, *http.Request) { served = true }))

	rr := httptest.NewRecorder()
	h.ServeHTTP(rr, reqWithToken("abc123"))

	if served {
		t.Fatal("the wrapped handler must not run when not logged in")
	}
	if rr.Code != http.StatusForbidden {
		t.Fatalf("status = %d, want 403", rr.Code)
	}
	body := rr.Body.String()
	if !strings.Contains(body, `"error":"product_gate"`) || !strings.Contains(body, `"reason":"not_logged_in"`) {
		t.Fatalf("body = %s, want product_gate/not_logged_in", body)
	}
}

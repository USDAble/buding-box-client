package permission

import (
	"strings"
	"testing"
)

// OCTO-FORK: invalid URLs remain denied regardless of mode, custom rules or remembered approval.
func TestWebFetchInvalidURLDenied(t *testing.T) {
	for _, mode := range []Mode{ModeInteractive, ModeAutoApprove, ModeStrict} {
		t.Run(string(mode), func(t *testing.T) {
			e, err := New("", "/work", mode)
			if err != nil {
				t.Fatal(err)
			}
			for _, input := range []map[string]any{
				nil,
				{"url": 123},
				{"url": ""},
				{"url": "https:///missing-host"},
				{"url": "https://github.com:invalid/"},
				{"url": "http://[broken]/"},
			} {
				for _, remembered := range []bool{false, true} {
					if remembered {
						e.Remember("web_fetch", input, Allow)
					}
					if got := e.Check("web_fetch", input); got != Deny {
						t.Errorf("input %v, remembered=%v: got %s, want deny", input, remembered, got)
					}
				}
				if reason := e.DenialReason("web_fetch", input); !strings.Contains(reason, "URL has no parseable hostname") {
					t.Errorf("input %v: incorrect denial reason %q", input, reason)
				}
			}
			for _, raw := range []string{"http://[::ffff:127.0.0.1]/", "http://[::ffff:7f00:1]/"} {
				input := map[string]any{"url": raw}
				e.Remember("web_fetch", input, Allow)
				if got := e.Check("web_fetch", input); got != Deny {
					t.Errorf("mapped loopback %q with remembered allow: got %s, want deny", raw, got)
				}
			}
			for _, raw := range []string{"https://github.com/", "github.com/path", "https://GITHUB.COM:443/path"} {
				if got := e.Check("web_fetch", map[string]any{"url": raw}); got != Allow {
					t.Errorf("valid URL %q: got %s, want allow", raw, got)
				}
			}
			want := Ask
			if mode == ModeAutoApprove {
				want = Allow
			} else if mode == ModeStrict {
				want = Deny
			}
			if got := e.Check("web_fetch", map[string]any{"url": "https://unknown.example/"}); got != want {
				t.Errorf("unknown valid host: got %s, want %s", got, want)
			}
			e.rules["web_fetch"] = []Rule{{Decision: Allow}}
			if got := e.Check("web_fetch", map[string]any{"url": "https:///missing-host"}); got != Deny {
				t.Errorf("blanket allow with invalid URL: got %s, want deny", got)
			}
		})
	}
}

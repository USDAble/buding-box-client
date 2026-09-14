package productruntime_test

import (
	"encoding/json"
	"net/http"
	"strings"
	"testing"

	"github.com/open-octo/octo-agent/internal/sensitive"
)

// These tests drive POST /api/product/sensitive/check through the REAL server
// over a real loopback socket, for the reason dict_route_test.go does: the
// composer had called this URL since before the fork existed and nothing was
// registered for it (V-24), so the failure mode was unreachability, not wrong
// logic. Four assertions below are about what the ANSWER contains rather than
// about the status code:
//
//   - `masked` is asserted on a MISS too, because "the field is always there" is
//     the contract (§2.12) and the archived handler omitted it when no engine
//     was wired - a shape only the frontend would have noticed, at the moment it
//     was initialising;
//   - the text AROUND the masked word, because `***` alone would also be
//     produced by an implementation that masks the whole string;
//   - the bytes of a miss, because a "harmless" trim or normalisation on the way
//     through would silently rewrite what the user typed;
//   - the user dictionary, because the engine caches and a route holding its own
//     read of the file would answer correctly and still disagree with the turn.

// checkSensitive issues the check and decodes the answer, presuming only that
// it is JSON.
func checkSensitive(t *testing.T, m *mountedHarness, text string) (int, map[string]any) {
	t.Helper()
	status, raw := m.request(t, http.MethodPost, "/api/product/sensitive/check",
		map[string]any{"text": text}, nil)
	var body map[string]any
	if len(raw) > 0 {
		if err := json.Unmarshal(raw, &body); err != nil {
			t.Fatalf("check body is not JSON (%v): %.200s", err, raw)
		}
	}
	return status, body
}

// maskedText reads the masked field, insisting it is PRESENT. A missing field
// decodes identically to an empty one, and the difference is the whole point of
// this assertion.
func maskedText(t *testing.T, body map[string]any) string {
	t.Helper()
	v, ok := body["masked"]
	if !ok {
		t.Fatalf("the answer has no masked field: %v", body)
	}
	s, ok := v.(string)
	if !ok {
		t.Fatalf("masked is not a string: %v", v)
	}
	return s
}

// TestTheCheckRouteSitsBehindTheGate is the V-24 lesson applied to this route:
// registered through the MountAPI seam, it inherits requireAuth and the window
// token like every other product route. The second half is what makes it about
// the gate rather than about the route being broken - presenting the token must
// work.
func TestTheCheckRouteSitsBehindTheGate(t *testing.T) {
	const header = "X-Octo-Window-Token"
	const token = "3f2a1b0c9d8e7f605142332415061728293a3b3c4d4e4f505152535455565758"
	m := newMountedHarnessWithToken(t, token)

	status, raw := m.request(t, http.MethodPost, "/api/product/sensitive/check",
		map[string]any{"text": "今天天气不错"}, nil)
	if status != http.StatusForbidden {
		t.Fatalf("check without the token = %d, want 403 (body: %.200s)", status, raw)
	}
	if !strings.Contains(strings.ToLower(string(raw)), "product_gate") {
		t.Errorf("the refusal is not product_gate: %.200s", raw)
	}

	status, raw = m.request(t, http.MethodPost, "/api/product/sensitive/check",
		map[string]any{"text": "今天天气不错"}, map[string]string{header: token})
	if status != http.StatusOK {
		t.Fatalf("check with the token = %d, want 200 (body: %.200s)", status, raw)
	}
}

// TestTheCheckAnswersTheMaskedForm: a hit returns the text with the word
// replaced, keeping everything around it. The surrounding characters are what
// tell "masked the word" apart from "masked the message", and the composer
// substitutes the answer into the input box verbatim - so a truncated answer
// would silently delete the user's sentence.
func TestTheCheckAnswersTheMaskedForm(t *testing.T) {
	m := newMountedHarness(t)

	status, body := checkSensitive(t, m, "我要开发票谢谢")
	if status != http.StatusOK {
		t.Fatalf("check = %d, want 200 (body: %v)", status, body)
	}
	if hit, _ := body["hit"].(bool); !hit {
		t.Fatalf("hit = %v, want true (body: %v)", body["hit"], body)
	}
	got := maskedText(t, body)
	if !strings.Contains(got, sensitive.Mask) {
		t.Errorf("masked = %q, want the word replaced by %q", got, sensitive.Mask)
	}
	if !strings.HasPrefix(got, "我要开") || !strings.HasSuffix(got, "谢谢") {
		t.Errorf("masked = %q, want the text around the word kept (前言与后语逐字不变)", got)
	}
	if strings.Contains(got, "发票") {
		t.Errorf("masked = %q, the word is still there", got)
	}
}

// TestAMissAnswersTheOriginalWithTheFieldAlwaysPresent is contract §2.12's
// "masked 字段始终在": on a miss the answer carries the original text, so the
// composer never has to guard against an absent field.
//
// The archived handler answered `{"hit": false}` alone when no engine was
// wired. That shape survives every hit test in this file and breaks the one
// case the frontend has no branch for.
func TestAMissAnswersTheOriginalWithTheFieldAlwaysPresent(t *testing.T) {
	m := newMountedHarness(t)

	// Whitespace, a tab and a newline on purpose: a miss must be a no-op down to
	// the byte, so a helper that trims on the way through is caught here rather
	// than in the input box.
	const text = "今天天气不错 \t好\n"
	status, body := checkSensitive(t, m, text)
	if status != http.StatusOK {
		t.Fatalf("check = %d, want 200 (body: %v)", status, body)
	}
	if hit, _ := body["hit"].(bool); hit {
		t.Fatalf("hit = true for a plain sentence (body: %v)", body)
	}
	if got := maskedText(t, body); got != text {
		t.Errorf("masked = %q, want %q byte for byte", got, text)
	}
}

// TestTheCheckOnEmptyTextIsNotAnError: §2.12 registers no business error for
// this route. An empty text is a miss, not a 400 - the composer calls this on
// every keystroke-triggered send, and an error path there would surface as a
// toast on a perfectly ordinary action.
func TestTheCheckOnEmptyTextIsNotAnError(t *testing.T) {
	m := newMountedHarness(t)

	status, body := checkSensitive(t, m, "")
	if status != http.StatusOK {
		t.Fatalf("check on empty text = %d, want 200 (body: %v)", status, body)
	}
	if hit, _ := body["hit"].(bool); hit {
		t.Fatalf("hit = true for empty text (body: %v)", body)
	}
	if got := maskedText(t, body); got != "" {
		t.Errorf("masked = %q, want the empty string", got)
	}
}

// TestTheCheckReadsTheWordListTheTurnPathReads is the criterion that separates
// "the route answers" from "the route consulted the same engine". The engine
// caches and reloads on the file's mod time, so a route that built its own
// engine - or read the file itself - would answer correctly here while the
// turn path masked a different list (开发规范 §3.8: one owner per fact).
//
// The write goes through the dict route rather than the file so the road under
// test is the one the library page uses.
func TestTheCheckReadsTheWordListTheTurnPathReads(t *testing.T) {
	m := newMountedHarness(t)
	const word = "香辣鸡腿"

	if status, body := checkSensitive(t, m, word); status != http.StatusOK {
		t.Fatalf("check = %d, want 200", status)
	} else if hit, _ := body["hit"].(bool); hit {
		t.Fatalf("the word hits before it was written (body: %v); the test would pass vacuously", body)
	}

	if status, body := putDict(t, m, word); status != http.StatusOK {
		t.Fatalf("PUT dict = %d, want 200 (body: %v)", status, body)
	}

	status, body := checkSensitive(t, m, "我想吃"+word)
	if status != http.StatusOK {
		t.Fatalf("check = %d, want 200", status)
	}
	if hit, _ := body["hit"].(bool); !hit {
		t.Fatalf("hit = false after the word was added through the route (body: %v) - the check reads a different word list than the one written", body)
	}
	if got := maskedText(t, body); !strings.Contains(got, sensitive.Mask) {
		t.Errorf("masked = %q, want the new word replaced by %q", got, sensitive.Mask)
	}
}

// TestTheSwitchDecidesAndRecovers pins the check's OTHER input - the user's
// preference (需求 D1 rule 2) - and it exists because mutation testing found the
// hole: with the gate stubbed in internal/server, every refusal nail stayed green
// while the real `SensitiveInputGate` ignored the switch entirely.
//
// The preference is written through PUT /api/product/prefs, the route PR-6b1
// registered, rather than by editing state: the claim is "what the user turned
// off is what this route reads". The word is the same one the first call reports
// as a hit, so the only thing that changed between the two answers is the switch.
//
// The third call is the recovery half (开发规范 §3.9): turning the check back on
// must take effect without a restart, which is why the gate reads the preference
// per call instead of at assembly.
func TestTheSwitchDecidesAndRecovers(t *testing.T) {
	m := newMountedHarness(t)
	const text = "我要开发票"

	status, body := checkSensitive(t, m, text)
	if status != http.StatusOK {
		t.Fatalf("check = %d, want 200 (body: %v)", status, body)
	}
	if hit, _ := body["hit"].(bool); !hit {
		t.Fatalf("hit = false before the switch was touched (body: %v); the test would pass vacuously", body)
	}

	if status, raw := m.request(t, http.MethodPut, "/api/product/prefs",
		map[string]any{"inputSensitiveCheck": false}, nil); status != http.StatusOK {
		t.Fatalf("prefs off = %d, want 200 (body: %.200s)", status, raw)
	}

	status, body = checkSensitive(t, m, text)
	if status != http.StatusOK {
		t.Fatalf("check with the switch off = %d, want 200 (body: %v)", status, body)
	}
	if hit, _ := body["hit"].(bool); hit {
		t.Errorf("hit = true with the switch off (body: %v) - the check is not reading the user's preference", body)
	}
	if got := maskedText(t, body); got != text {
		t.Errorf("masked = %q with the switch off, want %q untouched", got, text)
	}

	if status, raw := m.request(t, http.MethodPut, "/api/product/prefs",
		map[string]any{"inputSensitiveCheck": true}, nil); status != http.StatusOK {
		t.Fatalf("prefs on = %d, want 200 (body: %.200s)", status, raw)
	}

	status, body = checkSensitive(t, m, text)
	if status != http.StatusOK {
		t.Fatalf("check after turning it back on = %d, want 200 (body: %v)", status, body)
	}
	if hit, _ := body["hit"].(bool); !hit {
		t.Errorf("hit = false after the switch was turned back on (body: %v) - the gate cached the preference", body)
	}
}

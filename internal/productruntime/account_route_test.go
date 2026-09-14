package productruntime_test

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/open-octo/octo-agent/internal/productruntime"
	"github.com/open-octo/octo-agent/internal/productstate"
)

// These tests drive the two account-editing routes through the REAL server over
// a real loopback socket (mountHarness), because this closure's failure mode is
// unreachability: PR-6b's whole defect was five routes that existed in the
// frontend and were mounted nowhere (V-24). A handler unit test cannot tell
// "the route works" from "the route is not registered".
//
// Two assertions here are deliberately about envelopes and disk bytes rather
// than about values:
//
//   - The envelope, because the same two codes that mean "refused" on this route
//     are FIELD-level on the login path, and the frontend reads body.code here.
//     A wrong envelope raises no error and shows the wrong sentence.
//   - The bytes, because "the nickname was not saved" has to mean the file is
//     unchanged, not that a later read happens to disagree.

// stateBytes reads data/product-state.json raw. The raw bytes are the point: a
// decoded struct cannot tell "never written" from "written and read back".
func stateBytes(t *testing.T, root string) string {
	t.Helper()
	raw, err := os.ReadFile(filepath.Join(root, "product-state.json"))
	if err != nil {
		t.Fatalf("read product-state.json: %v", err)
	}
	return string(raw)
}

// nicknameIn reads the nickname out of the raw state file, so an assertion can
// be made against what is on disk rather than against a returned projection.
func nicknameIn(t *testing.T, root string) string {
	t.Helper()
	var st struct {
		Account *struct {
			Nickname string `json:"nickname"`
		} `json:"account"`
	}
	if err := json.Unmarshal([]byte(stateBytes(t, root)), &st); err != nil {
		t.Fatalf("decode product-state.json: %v", err)
	}
	if st.Account == nil {
		return ""
	}
	return st.Account.Nickname
}

// writeUserDict puts the user's own word list in the data root. It is the layer
// that makes "命中词表" a fact about this machine rather than about the embedded
// list, so a test does not depend on which words ship in the binary.
func writeUserDict(t *testing.T, root string, words ...string) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(root, "sensitive-words.txt"), []byte(strings.Join(words, "\n")+"\n"), 0o600); err != nil {
		t.Fatalf("write sensitive-words.txt: %v", err)
	}
}

// setNickname is the request these tests make, narrowed to the two facts worth
// reading: the status and the decoded body (whose SHAPE is what half of them
// assert).
func setNickname(t *testing.T, m *mountedHarness, nickname string) (int, map[string]any) {
	t.Helper()
	status, raw := m.request(t, "PUT", "/api/product/nickname", map[string]any{"nickname": nickname}, nil)
	var body map[string]any
	if err := json.Unmarshal(raw, &body); err != nil {
		t.Fatalf("decode body %.200s: %v", raw, err)
	}
	return status, body
}

func TestAValidNicknameIsStoredAndReadBack(t *testing.T) {
	m := newMountedHarness(t)

	status, body := setNickname(t, m, "张三_99")
	if status != 200 {
		t.Fatalf("status = %d, want 200 (body: %v)", status, body)
	}

	// §2.6: the answer is {"state": …} and the frontend overwrites its store
	// with it, so the returned state is the second half of the judgement.
	state, ok := body["state"].(map[string]any)
	if !ok {
		t.Fatalf("no state in body: %v", body)
	}
	account, _ := state["account"].(map[string]any)
	if account == nil || account["nickname"] != "张三_99" {
		t.Fatalf("state.account.nickname = %v, want 张三_99", account)
	}
	if got := nicknameIn(t, m.root); got != "张三_99" {
		t.Fatalf("on disk nickname = %q, want 张三_99", got)
	}
}

// TestTheNicknameBoundsAreTheRequirementsNotTwentyCodepoints pins V-73. The rule
// that was in force was "non-empty and at most 20 code points, any characters",
// so every rejected case below was accepted by the server while the frontend
// refused it.
//
// The fixtures are web/src/lib/nickname.test.ts's, value for value. That test
// already existed and was green while this side was wrong — the defect survived
// because only one side was pinned to the shared list, so a change to either
// rule only had to satisfy its own copy. Mirroring the list here is what makes
// the pair a pair.
func TestTheNicknameBoundsAreTheRequirementsNotTwentyCodepoints(t *testing.T) {
	accepted := []string{
		"用户1234", "User", "_name_", "张三三", "aaaaaaaaaaaaaaaa", "字a1_",
		// 16 CJK characters: the count is code points, not bytes.
		"布丁盒子布丁盒子布丁盒子布丁盒子",
	}
	rejected := []struct {
		name     string
		nickname string
	}{
		{"one character is below the minimum", "a"},
		{"seventeen characters is above the maximum", "aaaaaaaaaaaaaaaaa"},
		{"hyphen is not in the allowed set", "用户-1"},
		{"space is not in the allowed set", "用户 1"},
		{"emoji is not in the allowed set", "🙂🙂"},
		{"empty is refused", ""},
	}

	for _, nickname := range accepted {
		t.Run("accepted/"+nickname, func(t *testing.T) {
			m := newMountedHarness(t)
			if status, body := setNickname(t, m, nickname); status != 200 {
				t.Fatalf("%q: status = %d, want 200 (body: %v)", nickname, status, body)
			}
			if got := nicknameIn(t, m.root); got != nickname {
				t.Fatalf("on disk nickname = %q, want %q", got, nickname)
			}
		})
	}

	for _, tc := range rejected {
		t.Run(tc.name, func(t *testing.T) {
			m := newMountedHarness(t)
			before := nicknameIn(t, m.root)

			status, body := setNickname(t, m, tc.nickname)
			if status != 400 {
				t.Fatalf("status = %d, want 400 (body: %v)", status, body)
			}
			if body["code"] != "nickname_format" {
				t.Fatalf("code = %v, want nickname_format", body["code"])
			}
			if _, present := body["fieldErrors"]; present {
				t.Fatalf("nickname refusals must be business-level, not fieldErrors: %v", body)
			}
			if after := nicknameIn(t, m.root); after != before {
				t.Fatalf("refused nickname was written: %q -> %q", before, after)
			}
		})
	}
}

// TestASensitiveNicknameIsRefusedAndLeavesNoTrace pins D3. The word list is the
// user's own file, so this cannot pass by accident off the embedded list, and
// the assertion is on the raw bytes: "refused" has to mean the file never
// learned the word.
func TestASensitiveNicknameIsRefusedAndLeavesNoTrace(t *testing.T) {
	m := newMountedHarness(t)
	writeUserDict(t, m.root, "测试词")
	before := stateBytes(t, m.root)

	status, body := setNickname(t, m, "张三测试词")
	if status != 400 {
		t.Fatalf("status = %d, want 400 (body: %v)", status, body)
	}
	if body["code"] != "nickname_sensitive" {
		t.Fatalf("code = %v, want nickname_sensitive (body: %v)", body["code"], body)
	}
	// The trap this file's header records: reusing writeFieldErrors would leave
	// body.code undefined and the UI would say 「昵称格式不正确」.
	if _, present := body["fieldErrors"]; present {
		t.Fatalf("a sensitive nickname must not arrive as fieldErrors: %v", body)
	}
	if after := stateBytes(t, m.root); after != before {
		t.Fatalf("the state file changed on a refused nickname:\nbefore: %s\nafter:  %s", before, after)
	}
	if strings.Contains(stateBytes(t, m.root), "测试词") {
		t.Fatal("the refused word reached the state file")
	}
}

// TestTheInputSwitchDoesNotTurnTheNicknameCheckOff is D3's second half: the
// check is always on. A user who turned input detection off must still be
// refused here — the switch governs input detection only (PR-6b2 wires that
// side), never this.
func TestTheInputSwitchDoesNotTurnTheNicknameCheckOff(t *testing.T) {
	m := newMountedHarness(t)
	writeUserDict(t, m.root, "测试词")

	status, body := m.request(t, "PUT", "/api/product/prefs", map[string]any{"inputSensitiveCheck": false}, nil)
	var prefsBody map[string]any
	_ = json.Unmarshal(body, &prefsBody)
	if status != 200 {
		t.Fatalf("prefs status = %d, want 200 (body: %s)", status, body)
	}

	status, refusal := setNickname(t, m, "张三测试词")
	if status != 400 || refusal["code"] != "nickname_sensitive" {
		t.Fatalf("with the switch off: status = %d, code = %v; want 400/nickname_sensitive", status, refusal["code"])
	}
}

// TestAnInnocentNicknameIsStoredByteForByte is the counter-nail: the rule must
// not have grown a normalisation step (a trim, a case fold) that silently
// rewrites what the user typed.
func TestAnInnocentNicknameIsStoredByteForByte(t *testing.T) {
	m := newMountedHarness(t)
	const nickname = "Zhang_San"

	if status, body := setNickname(t, m, nickname); status != 200 {
		t.Fatalf("status = %d, want 200 (body: %v)", status, body)
	}
	if got := nicknameIn(t, m.root); got != nickname {
		t.Fatalf("on disk nickname = %q, want %q", got, nickname)
	}
	if strings.Contains(stateBytes(t, m.root), "***") {
		t.Fatal("an accepted nickname must not be masked — D3 requires refusal, never a masked value")
	}
}

func TestPreferencesAreStoredFieldByField(t *testing.T) {
	m := newMountedHarness(t)

	type prefs struct {
		Locale              string `json:"locale"`
		DefaultChatMode     string `json:"defaultChatMode"`
		InputSensitiveCheck bool   `json:"inputSensitiveCheck"`
	}
	read := func() prefs {
		t.Helper()
		var st struct {
			Prefs prefs `json:"prefs"`
		}
		if err := json.Unmarshal([]byte(stateBytes(t, m.root)), &st); err != nil {
			t.Fatalf("decode product-state.json: %v", err)
		}
		return st.Prefs
	}

	// One field at a time, because "omitted means unchanged" is the shape the
	// frontend sends when a single control is edited (§2.7).
	steps := []struct {
		body map[string]any
		want func(p prefs) bool
		desc string
	}{
		{map[string]any{"locale": "en"}, func(p prefs) bool { return p.Locale == "en" }, "locale"},
		{map[string]any{"defaultChatMode": "privacy"}, func(p prefs) bool { return p.DefaultChatMode == "privacy" }, "defaultChatMode"},
		{map[string]any{"inputSensitiveCheck": false}, func(p prefs) bool { return !p.InputSensitiveCheck }, "inputSensitiveCheck"},
	}
	for _, step := range steps {
		status, raw := m.request(t, "PUT", "/api/product/prefs", step.body, nil)
		if status != 200 {
			t.Fatalf("%s: status = %d, want 200 (body: %.200s)", step.desc, status, raw)
		}
		if got := read(); !step.want(got) {
			t.Fatalf("%s: not stored; prefs on disk = %+v", step.desc, got)
		}
	}

	// The earlier edits must have survived the later ones — a handler that wrote
	// a zero value for every omitted field would pass each step above and lose
	// the previous one.
	got := read()
	if got.Locale != "en" || got.DefaultChatMode != "privacy" || got.InputSensitiveCheck {
		t.Fatalf("edits did not accumulate: %+v", got)
	}
}

func TestAPreferenceRefusalNamesTheFieldBesideTheCode(t *testing.T) {
	// §2.7's envelope: {"field": …, "code": "invalid_value"}. The frontend reads
	// body.field and files the message under that input itself (product.ts:550).
	// The sibling route PUT /api/product/locale now answers the same shape (V-72);
	// "unify them into fieldErrors" is still the wrong repair — invalid_value is a
	// local verdict, and fieldErrors is how this package relays the platform's.
	cases := []struct {
		name  string
		body  map[string]any
		field string
	}{
		{"unknown locale", map[string]any{"locale": "jp"}, "locale"},
		{"unknown chat mode", map[string]any{"defaultChatMode": "turbo"}, "defaultChatMode"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			m := newMountedHarness(t)
			before := stateBytes(t, m.root)

			status, raw := m.request(t, "PUT", "/api/product/prefs", tc.body, nil)
			var body map[string]any
			if err := json.Unmarshal(raw, &body); err != nil {
				t.Fatalf("decode body %.200s: %v", raw, err)
			}
			if status != 400 {
				t.Fatalf("status = %d, want 400 (body: %v)", status, body)
			}
			if body["field"] != tc.field {
				t.Fatalf("field = %v, want %v (body: %v)", body["field"], tc.field, body)
			}
			if body["code"] != "invalid_value" {
				t.Fatalf("code = %v, want invalid_value", body["code"])
			}
			if _, present := body["fieldErrors"]; present {
				t.Fatalf("prefs refusals must carry field beside code, not fieldErrors: %v", body)
			}
			if after := stateBytes(t, m.root); after != before {
				t.Fatal("a refused preference was written")
			}
		})
	}
}

// TestBothPreferenceRoutesRefuseTheSameLocaleTheSameWay pins V-72. The two routes
// used to answer the same refusal in two shapes — locale with fieldErrors, prefs
// with {"field": …, "code": …} — and each was green on its own, which is why the
// contradiction lived in the contract rather than in a test.
//
// Comparing the two BODIES against each other, not each against "400", is what
// makes this a nail: a repair that unified them the wrong way (both fieldErrors)
// would still return 400 for both, and only the equality below would notice.
func TestBothPreferenceRoutesRefuseTheSameLocaleTheSameWay(t *testing.T) {
	m := newMountedHarness(t)

	shapes := map[string]map[string]any{}
	for _, path := range []string{"/api/product/locale", "/api/product/prefs"} {
		status, raw := m.request(t, "PUT", path, map[string]any{"locale": "jp"}, nil)
		var body map[string]any
		if err := json.Unmarshal(raw, &body); err != nil {
			t.Fatalf("decode %s body %.200s: %v", path, raw, err)
		}
		if status != 400 {
			t.Fatalf("%s status = %d, want 400 (body: %v)", path, status, body)
		}
		shapes[path] = body
	}

	if !reflect.DeepEqual(shapes["/api/product/locale"], shapes["/api/product/prefs"]) {
		t.Fatalf("the same refused locale reads differently per route (V-72):\n  locale: %v\n  prefs:  %v",
			shapes["/api/product/locale"], shapes["/api/product/prefs"])
	}
	// And the agreed shape is the one the frontend's prefs parser reads
	// (product.ts:550: body.field, then body.code).
	if got := shapes["/api/product/locale"]; got["field"] != "locale" || got["code"] != "invalid_value" {
		t.Fatalf("agreed shape = %v, want {field: locale, code: invalid_value}", got)
	}
}

// TestAGateRefusesTheAccountRoutesWithoutTheWindowToken is not about the handler
// at all: it is the reachability half. Both routes are mounted through the same
// registrar as everything else, so the product gate has to cover them — a route
// that skipped the gate would be an unauthenticated write of the user's account.
func TestAGateRefusesTheAccountRoutesWithoutTheWindowToken(t *testing.T) {
	m := newMountedHarnessWithToken(t, "window-token-under-test")
	const header = "X-Octo-Window-Token"

	for _, path := range []string{"/api/product/nickname", "/api/product/prefs"} {
		status, _ := m.request(t, "PUT", path, map[string]any{"nickname": "张三四五"}, nil)
		if status != 403 {
			t.Fatalf("%s without the token = %d, want 403", path, status)
		}
		status, _ = m.request(t, "PUT", path, map[string]any{"nickname": "张三四五"}, map[string]string{header: "window-token-under-test"})
		if status == 403 {
			t.Fatalf("%s with the token = 403, want it through the gate", path)
		}
	}
}

// TestAMissingEngineRefusesTheNicknameRatherThanStoringItUnchecked is the
// fail-closed half of D3. "We could not check the word list" must not be
// reported as "the name was checked and is clean" (开发规范 §3.9), so a build
// that never got an engine refuses the edit — and the refusal has to leave the
// file untouched, or the unchecked name is already stored by the time the user
// reads the message.
//
// This runtime is built by hand rather than through the harness, because the
// harness assembles an engine on purpose: the shape under test is a build whose
// assembly forgot one.
func TestAMissingEngineRefusesTheNicknameRatherThanStoringItUnchecked(t *testing.T) {
	root := t.TempDir()
	t.Setenv("OCTO_DATA_ROOT", root)
	state, err := productstate.Open(productstate.Options{})
	if err != nil {
		t.Fatalf("productstate.Open: %v", err)
	}
	rt := productruntime.New(productruntime.Deps{State: state})
	srv := httptest.NewServer(rt.Handler())
	t.Cleanup(srv.Close)

	before := stateBytes(t, root)
	raw, err := json.Marshal(map[string]any{"nickname": "张三四五"})
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	req, err := http.NewRequest("PUT", srv.URL+"/api/product/nickname", bytes.NewReader(raw))
	if err != nil {
		t.Fatalf("new request: %v", err)
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := http.DefaultClient.Do(req)
	if resp != nil {
		defer resp.Body.Close()
	}
	if err != nil {
		t.Fatalf("PUT nickname: %v", err)
	}
	if resp.StatusCode != http.StatusInternalServerError {
		t.Fatalf("status = %d, want 500 — an unchecked name must not be accepted", resp.StatusCode)
	}
	if after := stateBytes(t, root); after != before {
		t.Fatal("the nickname was stored even though it could not be checked")
	}
}

// TestAMalformedNicknameThatIsAlsoSensitiveIsReportedAsMalformed pins the ORDER
// of the two checks. 「测试 词」 carries a space (so its shape is wrong) and
// normalises to a word in the list (so it is also a hit). Shape wins, because
// that is the one the user can fix from the message alone — and the order is
// invisible unless a case is both, which is why this one exists.
func TestAMalformedNicknameThatIsAlsoSensitiveIsReportedAsMalformed(t *testing.T) {
	m := newMountedHarness(t)
	writeUserDict(t, m.root, "测试词")

	status, body := setNickname(t, m, "测试 词")
	if status != 400 {
		t.Fatalf("status = %d, want 400 (body: %v)", status, body)
	}
	if body["code"] != "nickname_format" {
		t.Fatalf("code = %v, want nickname_format — the shape check must run before the word check", body["code"])
	}
	if got := nicknameIn(t, m.root); strings.Contains(got, "测试") {
		t.Fatalf("nickname on disk = %q, want it untouched", got)
	}
}

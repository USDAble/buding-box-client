// account.go owns the two account-editing routes:
// PUT /api/product/nickname and PUT /api/product/prefs (本地API契约 §2.6 / §2.7).
//
// WHY BOTH IN ONE FILE. They share the one thing that is easy to get wrong: the
// failure envelope. Neither route may answer with fieldErrors, even though both
// name a field — the nickname codes are business-level ({"code": …}) and
// invalid_value carries its field name as a sibling of the code
// ({"field": …, "code": …}). The frontend reads body.code and body.field
// respectively (product.ts:529, product.ts:550).
//
// The prefs half of that rule now covers the sibling route too: PUT
// /api/product/locale used to answer fieldErrors for its own invalid locale,
// which made §3's single invalid_value row describe two different shapes (V-72).
// Both go through writeValueRefusal now, so the code has exactly one shape.
//
// The trap is specific and it is silent: nickname_format and nickname_sensitive
// ARE in envelope.go's fieldLevelCodes, because that table serves the platform's
// answer on the login path (§1.4). Reusing writeFieldErrors here would leave
// body.code undefined, and the UI would fall back to its default —
// 「昵称格式不正确」 — so a nickname refused for hitting a sensitive word would be
// reported as a formatting mistake. No error is raised and the sentence is
// wrong. 本地API契约 §2.2 records the identical trap for invalid_phone.
//
// OCTO-FORK: 本地新增路由（`PR-6b1`）— see dev-docs-usdable/需求/20260911/开发计划.md §PR-6b1.
package productruntime

import (
	"net/http"
	"unicode"

	"github.com/open-octo/octo-agent/internal/productclient"
)

// The two refusal codes of 本地API契约 §2.6. They are local decisions — the word
// list is this build's own data/sensitive-words.txt plus its embedded list — so
// they are written here rather than forwarded from a platform answer.
//
// The third local code, invalid_value, lives in envelope.go beside the field-level
// table it is deliberately absent from (V-72); it now serves this route and the
// sibling PUT /api/product/locale both.
const (
	codeNicknameFormat    = "nickname_format"
	codeNicknameSensitive = "nickname_sensitive"
)

// handleNickname implements PUT /api/product/nickname (本地API契约 §2.6).
//
// The state in the answer is what was persisted, read back from storage — the
// frontend overwrites its shared store with it rather than updating locally
// (§2.6), so a silent write failure cannot leave the screen ahead of the disk.
func (rt *Runtime) handleNickname(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Nickname string `json:"nickname"`
	}
	if !decodeBody(w, r, &req) {
		return
	}

	// Shape before word list. A nickname that is both malformed and sensitive is
	// reported as malformed: that is the thing the user can fix from the message
	// without consulting anything else.
	if !validNickname(req.Nickname) {
		writeCode(w, http.StatusBadRequest, codeNicknameFormat, nil)
		return
	}

	switch rt.nicknameVerdict(req.Nickname) {
	case nicknameRefused:
		writeCode(w, http.StatusBadRequest, codeNicknameSensitive, nil)
		return
	case nicknameUncheckable:
		// Fail closed. D3 is a compliance rule, and "we could not check" must not
		// read as "we checked and it was clean" (开发规范 §3.9): a build without
		// the engine refuses the edit rather than storing an unchecked name.
		writeCode(w, http.StatusInternalServerError, productclient.CodeInternalError, nil)
		return
	}

	if err := rt.deps.State.SetNickname(req.Nickname); err != nil {
		writeCode(w, http.StatusInternalServerError, productclient.CodeInternalError, nil)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"state": rt.deps.State.PublicState()})
}

// handlePrefs implements PUT /api/product/prefs (本地API契约 §2.7).
//
// Every field is optional and an omitted one means "leave it alone" — the shape
// the frontend sends when it edits one control in the settings panel.
func (rt *Runtime) handlePrefs(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Locale              *string `json:"locale"`
		InputSensitiveCheck *bool   `json:"inputSensitiveCheck"`
	}
	if !decodeBody(w, r, &req) {
		return
	}

	if req.Locale != nil && !validLocale(*req.Locale) {
		writeValueRefusal(w, "locale")
		return
	}
	if err := rt.deps.State.SetPrefs(req.Locale, req.InputSensitiveCheck); err != nil {
		writeCode(w, http.StatusInternalServerError, productclient.CodeInternalError, nil)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"state": rt.deps.State.PublicState()})
}

// writeValueRefusal is the ONE writer for §2.5's and §2.7's shared refusal: the
// field name travels BESIDE the code, not inside a fieldErrors map. See this
// file's header for why the difference matters; envelope.go holds the constant
// and the reason it is absent from fieldLevelCodes.
func writeValueRefusal(w http.ResponseWriter, field string) {
	writeCode(w, http.StatusBadRequest, codeInvalidValue, map[string]any{"field": field})
}

// nicknameVerdict is the outcome of the compliance question.
type nicknameVerdict int

const (
	nicknameAccepted nicknameVerdict = iota
	nicknameRefused
	nicknameUncheckable
)

// nicknameVerdict reports whether the word list contains the nickname.
//
// The judgement is local and always on: D3 requires the check regardless of the
// input-detection switch (inputSensitiveCheck), so this reads the engine
// directly and never consults the preference. The engine is deliberately not
// consulted for the mask — a nickname is refused, never stored masked, or the
// user would end up with "***" as their name (D3).
func (rt *Runtime) nicknameVerdict(nickname string) nicknameVerdict {
	if rt.deps.Sensitive == nil {
		return nicknameUncheckable
	}
	if rt.deps.Sensitive.Filter(nickname).Matched() {
		return nicknameRefused
	}
	return nicknameAccepted
}

// validNickname bounds and constrains the nickname. It is the ONE rule for both
// paths that accept a nickname — the activation/login form and this route — so
// the two cannot drift apart.
//
// The rule is 需求 20260906 §「昵称」: 2–16 characters, drawn from Han
// ideographs, letters, digits and underscore. It is mirrored in
// web/src/lib/nickname.ts for responsiveness; that mirror says the server
// re-runs the same rule, so this is the copy that has to be right.
//
// WHAT WAS WRONG BEFORE (V-73). The rule here was "non-empty and at most 20
// code points, any characters at all" — so a one-character nickname, a name
// containing a space or an emoji, and a 17–20 character name all passed the
// server, while the frontend refused each of them. The bounds and the character
// set are now the requirement's, and whitespace is NOT trimmed away first: a
// leading space is not one of the allowed characters, and trimming would make
// this rule accept an input the mirror rejects.
func validNickname(nickname string) bool {
	runes := []rune(nickname)
	if len(runes) < 2 || len(runes) > 16 {
		return false
	}
	for _, c := range runes {
		if !isNicknameRune(c) {
			return false
		}
	}
	return true
}

// isNicknameRune is the allowed set: underscore, or any letter or digit.
//
// unicode.IsLetter covers Han as well as Latin/Greek/… , which matches the
// frontend's /^[\p{Script=Han}\p{L}\p{Nd}_]+$/u — Script=Han is a subset of L,
// so the two accept exactly the same characters.
func isNicknameRune(c rune) bool {
	return c == '_' || unicode.IsLetter(c) || unicode.IsDigit(c)
}

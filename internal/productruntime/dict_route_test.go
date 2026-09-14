package productruntime_test

import (
	"encoding/json"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/open-octo/octo-agent/internal/productclient"
	"github.com/open-octo/octo-agent/internal/sensitive"
)

// These tests drive the three dictionary routes through the REAL server over a
// real loopback socket (mountHarness), for the reason this whole package tests
// that road: the closure PR-6b2 completes failed as unreachability, not as wrong
// logic. The frontend had called these two URLs since before the fork existed
// and nothing was registered for them (V-24), while every handler-level
// assertion anyone could have written stayed green.
//
// Two of the assertions below are deliberately about bytes rather than about
// values:
//
//   - the FILE, because "the word was added" has to mean data/sensitive-words.txt
//     really contains it - the routes are an editor for that file, and a 200
//     with the word in the answer would also be produced by a handler that only
//     echoes what it was sent;
//   - the header block, because the cheapest implementation of "replace the
//     user layer" is to rewrite the whole file, which silently drops the
//     explanation the user wrote at the top of it.

// dictPath is where the routes must be writing. It is built from
// sensitive.DictFileName rather than from a literal, so this test fails if the
// route ever names a different file than the package that reads dictionaries.
func dictPath(root string) string {
	return filepath.Join(root, sensitive.DictFileName)
}

// dictBytes reads the user dictionary raw. The bytes are the point: a decoded
// list cannot tell "the write landed" from "the answer claimed it did".
func dictBytes(t *testing.T, root string) (string, bool) {
	t.Helper()
	raw, err := os.ReadFile(dictPath(root))
	if err != nil {
		if os.IsNotExist(err) {
			return "", false
		}
		t.Fatalf("read %s: %v", sensitive.DictFileName, err)
	}
	return string(raw), true
}

// getDict issues the read and decodes the body without presuming its shape.
func getDict(t *testing.T, m *mountedHarness) (int, map[string]any) {
	t.Helper()
	status, raw := m.request(t, http.MethodGet, "/api/product/sensitive/dict", nil, nil)
	var body map[string]any
	if len(raw) > 0 {
		if err := json.Unmarshal(raw, &body); err != nil {
			t.Fatalf("GET dict body is not JSON (%v): %.200s", err, raw)
		}
	}
	return status, body
}

// putDict issues the replace call and decodes the answer, so the tests below
// read like the page does: send a list, read back what was stored.
func putDict(t *testing.T, m *mountedHarness, user ...string) (int, map[string]any) {
	t.Helper()
	if user == nil {
		user = []string{}
	}
	status, raw := m.request(t, http.MethodPut, "/api/product/sensitive/dict",
		map[string]any{"user": user}, nil)
	var body map[string]any
	if len(raw) > 0 {
		if err := json.Unmarshal(raw, &body); err != nil {
			t.Fatalf("PUT dict body is not JSON (%v): %.200s", err, raw)
		}
	}
	return status, body
}

// userList reads the user array out of a decoded body, telling an empty array
// apart from a missing or null one - the difference the frontend renders as
// "no words of your own" vs a crash.
func userList(t *testing.T, body map[string]any) []any {
	t.Helper()
	v, ok := body["user"]
	if !ok {
		t.Fatalf("body has no user field: %v", body)
	}
	if v == nil {
		t.Fatal(`user is null; the contract says an array, and the page iterates it`)
	}
	list, ok := v.([]any)
	if !ok {
		t.Fatalf("user is %T, want an array", v)
	}
	return list
}

// builtinWord returns a word from the embedded list, so the "cannot delete a
// built-in word" assertions do not depend on a literal that a future edit to
// default-words.txt could quietly remove.
func builtinWord(t *testing.T) string {
	t.Helper()
	words := sensitive.BuiltinWords()
	if len(words) == 0 {
		t.Fatal("the embedded dictionary is empty; these tests would pass vacuously")
	}
	return words[0]
}

// TestDictRoutesReadBothLayers is 本地API契约 §2.9: the built-in list (read-only,
// embedded) and the user list (the file), and nothing else.
func TestDictRoutesReadBothLayers(t *testing.T) {
	m := newMountedHarness(t)
	writeUserDict(t, m.root, "苹果", "香蕉")

	status, body := getDict(t, m)
	if status != http.StatusOK {
		t.Fatalf("GET dict = %d, want 200 (body: %v)", status, body)
	}

	builtin, ok := body["builtin"].([]any)
	if !ok || len(builtin) == 0 {
		t.Fatalf("builtin is %T/%v, want the embedded list", body["builtin"], body["builtin"])
	}
	sawBuiltin := false
	for _, w := range builtin {
		if w == "发票" {
			sawBuiltin = true
		}
	}
	if !sawBuiltin {
		t.Error("the built-in list does not carry 发票, which the requirement's baseline requires")
	}

	if got := userList(t, body); len(got) != 2 || got[0] != "苹果" || got[1] != "香蕉" {
		t.Errorf("user = %v, want the file's two words in file order", got)
	}
}

// TestMissingUserDictIsAnEmptyListNotAnError pins §2.9's legal state: a drive
// whose user never added a word has no dictionary file, and that is an empty
// user layer - not a failure, and not a reason to create a file.
//
// The "not created" half is the one that matters: writing the default at startup
// is what 开发规范 §3.9.1 forbids, because it turns "the user has no words" into
// a file the user did not write and then has to be reconciled on the next edit.
func TestMissingUserDictIsAnEmptyListNotAnError(t *testing.T) {
	m := newMountedHarness(t)

	if _, exists := dictBytes(t, m.root); exists {
		t.Fatal("the harness data root already has a dictionary file; this test would prove nothing")
	}

	status, body := getDict(t, m)
	if status != http.StatusOK {
		t.Fatalf("GET dict with no user file = %d, want 200 (body: %v)", status, body)
	}
	if got := userList(t, body); len(got) != 0 {
		t.Errorf("user = %v, want an empty array", got)
	}
	if _, exists := dictBytes(t, m.root); exists {
		t.Error("reading the dictionary created the file; absence must mean 'use the built-in list only'")
	}
}

// TestPutWritesTheWordToDisk is L-D4b's happy path, asserted on the file rather
// than on the answer: the routes are an editor for data/sensitive-words.txt, so
// "the word was added" has to mean the drive holds it.
func TestPutWritesTheWordToDisk(t *testing.T) {
	m := newMountedHarness(t)

	status, body := putDict(t, m, "苹果")
	if status != http.StatusOK {
		t.Fatalf("PUT dict = %d, want 200 (body: %v)", status, body)
	}
	users := userList(t, body)
	if len(users) != 1 || users[0] != "苹果" {
		t.Errorf("user = %v, want the word that was just written", users)
	}
	// PUT answers with the user list only; builtin is the read route's field
	// (§2.9 vs §2.10). A response carrying it would mean one shape is being used
	// for two routes, which the frontend's two call sites do not expect.
	if _, extra := body["builtin"]; extra {
		t.Error("the PUT answer carries builtin; §2.10 answers with user alone")
	}

	raw, exists := dictBytes(t, m.root)
	if !exists {
		t.Fatalf("the write did not create %s", sensitive.DictFileName)
	}
	if !strings.Contains(raw, "苹果\n") {
		t.Errorf("the file does not hold the word:\n%s", raw)
	}

	// Read it back through the route, which is how the page confirms a save.
	status, body = getDict(t, m)
	if status != http.StatusOK {
		t.Fatalf("GET dict after the write = %d, want 200", status)
	}
	if users := userList(t, body); len(users) != 1 || users[0] != "苹果" {
		t.Errorf("read back %v, want the written word", users)
	}
}

// TestAWordShapedLikeABuiltinCannotBeAdded is L-D4b's other half: the user
// deletes the built-in entry, saves, and the word still filters.
//
// It is asserted twice on purpose. The list half shows the entry is not stored;
// the matcher half shows the word is still effective, which is the property the
// user actually cares about - a list that looks clean while the matcher lost its
// floor would be worse than the original complaint.
func TestAWordShapedLikeABuiltinCannotBeAdded(t *testing.T) {
	m := newMountedHarness(t)
	builtin := builtinWord(t)

	status, body := putDict(t, m, "苹果", builtin)
	if status != http.StatusOK {
		t.Fatalf("PUT dict = %d, want 200 (body: %v)", status, body)
	}
	for _, w := range userList(t, body) {
		if w == builtin {
			t.Errorf("the answer stored the built-in word %q in the user layer", builtin)
		}
	}
	if raw, _ := dictBytes(t, m.root); strings.Contains(raw, builtin+"\n") {
		t.Errorf("the built-in word %q reached the file:\n%s", builtin, raw)
	}

	// The matcher still knows it. m.engine is the very instance the turn path
	// was given (Config.SensitiveEngine in mount_test.go), so this is the same
	// reader the screen consults; that a turn goes through it is nailed
	// separately in internal/server (L-D2).
	if got := m.engine.Filter("这里有个" + builtin).Text; !strings.Contains(got, sensitive.Mask) {
		t.Errorf("Filter(%q) = %q, want the built-in word masked", builtin, got)
	}
}

// TestPutRefusesAWordThatNormalizesToNothing is §2.10's only business refusal.
//
// A word that normalizes to nothing matches every text, so accepting it would
// turn every answer into `***`. The whole write is refused rather than the entry
// dropped: silently storing less than asked is how the page and the file start
// disagreeing, and the user has no way to tell which one is right.
func TestPutRefusesAWordThatNormalizesToNothing(t *testing.T) {
	for _, word := range []string{"   ", "！@#", ""} {
		t.Run(word, func(t *testing.T) {
			m := newMountedHarness(t)
			writeUserDict(t, m.root, "苹果")
			before, _ := dictBytes(t, m.root)

			status, raw := m.request(t, http.MethodPut, "/api/product/sensitive/dict",
				map[string]any{"user": []string{"香蕉", word}}, nil)
			if status != http.StatusBadRequest {
				t.Fatalf("PUT with %q = %d, want 400 (body: %.200s)", word, status, raw)
			}

			var body map[string]any
			if err := json.Unmarshal(raw, &body); err != nil {
				t.Fatalf("error body is not JSON: %.200s", raw)
			}
			if body["code"] != "invalid_word" {
				t.Errorf("code = %v, want invalid_word", body["code"])
			}
			// The offending word travels beside the code so the page can point at
			// the row the user has to fix.
			if got, ok := body["word"]; !ok || got != word {
				t.Errorf("word = %v (present: %v), want %q", got, ok, word)
			}
			// The envelope is business-level: fieldLevelCodes maps codes onto
			// input boxes, and a list entry has no box (see V-75).
			if _, fields := body["fieldErrors"]; fields {
				t.Error("the refusal used the field-level envelope; the dictionary page reads code + word")
			}

			if after, _ := dictBytes(t, m.root); after != before {
				t.Errorf("a refused write changed the file:\nbefore %q\nafter  %q", before, after)
			}
		})
	}
}

// TestPutDedupesByNormalizedForm pins §2.10's "server-side dedup + normalization"
// and, just as importantly, that normalization is for COMPARISON only: the file
// keeps the spelling the user typed, so a word list does not quietly rewrite
// itself into a form the user did not write.
//
// The pair is one word written two ways, which is what the matcher itself
// considers one word (normalizeText strips whitespace and symbols). The
// frontend's normalizeWord mirrors this rule; the two are kept in step by
// sensitiveDict.test.ts on one side and this test on the other.
func TestPutDedupesByNormalizedForm(t *testing.T) {
	m := newMountedHarness(t)

	status, body := putDict(t, m, "香 蕉", "香蕉")
	if status != http.StatusOK {
		t.Fatalf("PUT dict = %d, want 200 (body: %v)", status, body)
	}
	users := userList(t, body)
	if len(users) != 1 || users[0] != "香 蕉" {
		t.Errorf("user = %v, want just the first spelling of the one word", users)
	}
	if raw, _ := dictBytes(t, m.root); !strings.Contains(raw, "香 蕉\n") {
		t.Errorf("the file lost the user's spelling:\n%s", raw)
	}
}

// TestImportMergesInsteadOfReplacing is L-D4c's semantics: import adds to the
// user layer. A wholesale overwrite would look identical in the answer (added /
// skipped) while destroying every word the user had already collected, which is
// the reason the two routes exist as separate shapes at all.
func TestImportMergesInsteadOfReplacing(t *testing.T) {
	m := newMountedHarness(t)
	writeUserDict(t, m.root, "苹果")

	status, body := m.post(t, "/api/product/sensitive/dict/import",
		map[string]any{"words": []string{"苹果", "香蕉"}, "dryRun": false})
	if status != http.StatusOK {
		t.Fatalf("import = %d, want 200 (body: %v)", status, body)
	}
	if body["added"] != float64(1) || body["skipped"] != float64(1) {
		t.Errorf("added/skipped = %v/%v, want 1/1", body["added"], body["skipped"])
	}

	raw, _ := dictBytes(t, m.root)
	if !strings.Contains(raw, "苹果\n") || !strings.Contains(raw, "香蕉\n") {
		t.Errorf("import did not merge; the file is:\n%s", raw)
	}
}

// TestImportPreviewDoesNotTouchTheDisk is the dry run the page shows before the
// user confirms. The counts must be real (they are what the confirmation screen
// displays) while the file stays untouched - and, on a root that has no
// dictionary yet, not even created.
func TestImportPreviewDoesNotTouchTheDisk(t *testing.T) {
	m := newMountedHarness(t)
	writeUserDict(t, m.root, "苹果")
	before, _ := dictBytes(t, m.root)

	status, body := m.post(t, "/api/product/sensitive/dict/import",
		map[string]any{"words": []string{"苹果", "香蕉"}, "dryRun": true})
	if status != http.StatusOK {
		t.Fatalf("preview = %d, want 200 (body: %v)", status, body)
	}
	if body["added"] != float64(1) || body["skipped"] != float64(1) {
		t.Errorf("preview added/skipped = %v/%v, want the same 1/1 the real import reports", body["added"], body["skipped"])
	}
	if after, _ := dictBytes(t, m.root); after != before {
		t.Errorf("the preview wrote to the file:\nbefore %q\nafter  %q", before, after)
	}
}

// TestImportCountsABuiltinConflictAsSkipped pins the asymmetry between the two
// write routes, which is the thing a reader is most likely to "unify" later:
// PUT REFUSES a blank word (the user asked for something unstorable), while
// import SKIPS one, and both SKIP a built-in duplicate. Failing a whole bulk
// import over one unusable line would make pasting a file useless.
func TestImportCountsABuiltinConflictAsSkipped(t *testing.T) {
	m := newMountedHarness(t)
	builtin := builtinWord(t)

	status, body := m.post(t, "/api/product/sensitive/dict/import",
		map[string]any{"words": []string{builtin, "   ", "香蕉"}, "dryRun": false})
	if status != http.StatusOK {
		t.Fatalf("import = %d, want 200 (body: %v)", status, body)
	}
	if body["added"] != float64(1) || body["skipped"] != float64(2) {
		t.Errorf("added/skipped = %v/%v, want 1/2 (built-in conflict and blank line both skipped)",
			body["added"], body["skipped"])
	}
	raw, _ := dictBytes(t, m.root)
	if strings.Contains(raw, builtin+"\n") {
		t.Errorf("the built-in word %q reached the user layer:\n%s", builtin, raw)
	}
}

// TestAWordWrittenThroughTheRouteReachesTheMatcher is the assertion the whole
// PR exists for: "the effective word list" is one fact, and the file the routes
// edit is the file the matcher reads.
//
// It is not enough for the write to succeed. The engine caches, so a route that
// wrote to a path the engine never looks at would answer 200, list the word back
// on the next GET, and never filter anything - the failure V-24 describes, one
// layer down. m.engine is the instance the turn path was given (mount_test.go
// passes the same pointer to both), so a hit here means the turn path sees it.
func TestAWordWrittenThroughTheRouteReachesTheMatcher(t *testing.T) {
	m := newMountedHarness(t)
	const word = "香辣鸡腿"

	if got := m.engine.Filter(word).Text; got != word {
		t.Fatalf("the word is already filtered before the write (%q); the test would pass vacuously", got)
	}

	status, body := putDict(t, m, word)
	if status != http.StatusOK {
		t.Fatalf("PUT dict = %d, want 200 (body: %v)", status, body)
	}

	if got := m.engine.Filter("我要吃" + word).Text; !strings.Contains(got, sensitive.Mask) {
		t.Errorf("Filter after the write = %q, want the new word masked - the route wrote a file the matcher does not read", got)
	}
}

// TestAllThreeDictRoutesSitBehindTheGate is the V-24 lesson applied to the new
// routes: they are registered through the MountAPI seam, so they inherit
// requireAuth and the window token like every other product route. A route
// reachable without the token would let a page that never authenticated read and
// rewrite the word list.
//
// The last assertion is what makes this about the gate rather than about the
// routes being broken: presenting the token must work.
func TestAllThreeDictRoutesSitBehindTheGate(t *testing.T) {
	const header = "X-Octo-Window-Token"
	const token = "3f2a1b0c9d8e7f605142332415061728293a3b3c4d4e4f505152535455565758"
	m := newMountedHarnessWithToken(t, token)

	calls := []struct {
		name, method, path string
		body               any
	}{
		{"read", http.MethodGet, "/api/product/sensitive/dict", nil},
		{"replace", http.MethodPut, "/api/product/sensitive/dict", map[string]any{"user": []string{"苹果"}}},
		{"import", http.MethodPost, "/api/product/sensitive/dict/import", map[string]any{"words": []string{"苹果"}}},
	}
	for _, c := range calls {
		t.Run(c.name, func(t *testing.T) {
			status, raw := m.request(t, c.method, c.path, c.body, nil)
			if status != http.StatusForbidden {
				t.Fatalf("%s %s without the token = %d, want 403 (body: %.200s)", c.method, c.path, status, raw)
			}
			if !strings.Contains(string(raw), "product_gate") {
				t.Errorf("the refusal is not product_gate: %.200s", raw)
			}
			if _, exists := dictBytes(t, m.root); exists {
				t.Error("a refused call wrote the dictionary")
			}
		})
	}

	status, raw := m.request(t, http.MethodGet, "/api/product/sensitive/dict", nil, map[string]string{header: token})
	if status != http.StatusOK {
		t.Fatalf("GET dict with the token = %d, want 200 (body: %.200s)", status, raw)
	}
}

// TestPutKeepsTheHeaderCommentBlock is a reverse nail against the cheapest way
// to implement "replace the user layer": rewriting the file from the new list
// alone. That version passes every other test in this file while losing the
// explanation the user wrote at the top of their own word list - and 需求 D4
// says the file is the user's.
//
// The trade-off is the other half and is recorded in 本地API契约 §2.10: comments
// BELOW the first word are not preserved. This asserts only what was promised.
func TestPutKeepsTheHeaderCommentBlock(t *testing.T) {
	m := newMountedHarness(t)
	const header = "# 我自己加的说明\n# 第二行\n"
	if err := os.WriteFile(dictPath(m.root), []byte(header+"\n旧词\n"), 0o644); err != nil {
		t.Fatalf("seed the dictionary: %v", err)
	}

	status, body := putDict(t, m, "苹果")
	if status != http.StatusOK {
		t.Fatalf("PUT dict = %d, want 200 (body: %v)", status, body)
	}

	raw, _ := dictBytes(t, m.root)
	if !strings.Contains(raw, header) {
		t.Errorf("the header comment block was not preserved:\n%s", raw)
	}
	if strings.Contains(raw, "旧词") {
		t.Errorf("the replaced word is still in the file:\n%s", raw)
	}
	if !strings.Contains(raw, "苹果\n") {
		t.Errorf("the new word is not in the file:\n%s", raw)
	}
}

// TestPutLeavesNoTemporaryFileBehind pins the visible half of the atomic write:
// after a successful replace there is no debris in the data root. A yanked U
// disk mid-write must not leave a half-written dictionary next to the user's
// data, and the next startup must not read one as the word list.
//
// WHAT THIS DOES NOT PROVE, and the mutation run said so out loud: replacing
// atomicfile.WriteFile with a plain os.WriteFile keeps THIS test green, because
// a single write() leaves no debris either. Atomicity itself (temp file in the
// same directory, then rename) is owned by internal/atomicfile and covered by
// its own tests; the verdict that this route must USE that implementation rather
// than carry a second one is a 复用 decision (§3.5), recorded in 开发计划 §PR-6b2.
// What this test does hold down is the debris class of bug - a hand-rolled
// version that forgets to clean up its temp file on the success path.
func TestPutLeavesNoTemporaryFileBehind(t *testing.T) {
	m := newMountedHarness(t)

	status, body := putDict(t, m, "苹果")
	if status != http.StatusOK {
		t.Fatalf("PUT dict = %d, want 200 (body: %v)", status, body)
	}

	entries, err := os.ReadDir(m.root)
	if err != nil {
		t.Fatalf("read the data root: %v", err)
	}
	for _, e := range entries {
		if strings.HasSuffix(e.Name(), ".tmp") {
			t.Errorf("the write left %q behind", e.Name())
		}
	}
}

// TestPutDoesNotChangeTheDictionaryFileMode pins a small decision with a long
// tail: the write lands with 0o644, the mode the seed file in
// packaging/portable/data ships with. The other stores under the data root use
// 0o600 because they hold secrets; this file is the user's own word list and
// holds none, and an edit that silently narrowed it would leave the file
// unreadable to anything but the account that happened to press save.
//
// Skipped on Windows: FAT32/exFAT carry no POSIX permission bits, so the
// assertion there is meaningless rather than false - the same reason V-64 ①
// stopped asserting 0600 on Windows instead of changing the code.
func TestPutDoesNotChangeTheDictionaryFileMode(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("the portable drive has no POSIX permission bits")
	}
	m := newMountedHarness(t)
	if err := os.WriteFile(dictPath(m.root), []byte("旧词\n"), 0o644); err != nil {
		t.Fatalf("seed the dictionary: %v", err)
	}

	status, body := putDict(t, m, "苹果")
	if status != http.StatusOK {
		t.Fatalf("PUT dict = %d, want 200 (body: %v)", status, body)
	}

	info, err := os.Stat(dictPath(m.root))
	if err != nil {
		t.Fatalf("stat the dictionary: %v", err)
	}
	if perm := info.Mode().Perm(); perm != 0o644 {
		t.Errorf("the dictionary is %#o after a write, want 0644 (the mode the shipped seed file has)", perm)
	}
}

// TestTheDictionaryIsTheFileTheRouteNames keeps the two ends of the path in one
// place. The routes may not reach into internal/server for the file name (the
// dependency runs the other way), so the name lives with the package that reads
// dictionaries (sensitive.DictFileName) and both sides resolve it through
// internal/datapath.
//
// Without this nail the split could silently become two facts: a route writing
// "sensitive-words.txt" while the engine reads something else is exactly the
// failure that answers 200, lists the word back, and filters nothing.
func TestTheDictionaryIsTheFileTheRouteNames(t *testing.T) {
	m := newMountedHarness(t)

	status, body := putDict(t, m, "苹果")
	if status != http.StatusOK {
		t.Fatalf("PUT dict = %d, want 200 (body: %v)", status, body)
	}

	raw, exists := dictBytes(t, m.root)
	if !exists {
		t.Fatalf("no %s under the data root after a successful write", sensitive.DictFileName)
	}
	if !strings.Contains(raw, "苹果\n") {
		t.Errorf("%s does not hold the written word:\n%s", sensitive.DictFileName, raw)
	}
}

// TestAFailedWriteIsAnInternalErrorNotARejectedWord pins the boundary this PR
// deliberately does not cross: the write freeze belongs to PR-7's watchdog, so
// until it lands an unwritable data root is an infrastructure fault and answers
// internal_error.
//
// The distinction is behavioural, not cosmetic. Reporting a business code would
// make the page blame the word the user typed, and the user would keep editing a
// perfectly good word list while the drive is the problem. The fault is injected
// portably - the data root is pointed at a path whose parent is a regular file,
// so no directory can be created there - because a read-only directory is a
// POSIX-only way to make a write fail (V-64 ②).
func TestAFailedWriteIsAnInternalErrorNotARejectedWord(t *testing.T) {
	m := newMountedHarness(t)

	blocker := filepath.Join(t.TempDir(), "not-a-directory")
	if err := os.WriteFile(blocker, []byte("x"), 0o644); err != nil {
		t.Fatalf("create the blocker file: %v", err)
	}
	t.Setenv("OCTO_DATA_ROOT", filepath.Join(blocker, "sub"))

	status, raw := m.request(t, http.MethodPut, "/api/product/sensitive/dict",
		map[string]any{"user": []string{"苹果"}}, nil)
	if status != http.StatusInternalServerError {
		t.Fatalf("PUT with an unusable data root = %d, want 500 (body: %.200s)", status, raw)
	}
	var body map[string]any
	if err := json.Unmarshal(raw, &body); err != nil {
		t.Fatalf("error body is not JSON: %.200s", raw)
	}
	if body["code"] != productclient.CodeInternalError {
		t.Errorf("code = %v, want %q", body["code"], productclient.CodeInternalError)
	}
	if body["code"] == "invalid_word" {
		t.Error("an unusable data root was reported as a rejected word")
	}
}

// sensitive.go owns the three dictionary-management routes: GET and PUT
// /api/product/sensitive/dict plus POST /api/product/sensitive/dict/import
// (本地API契约 §2.9 / §2.10 / §2.11).
//
// WHY THEY ARE HERE AND NOT IN internal/server. §0.1 names this package the
// authoritative landing point for the local product API, and only routes
// registered through the MountAPI seam inherit the product gate (L-B1). The
// archived implementation of these three lived in internal/server; carrying it
// over unchanged would have left them outside the gate and outside the list
// that both adapters (Mount and Handler) iterate - the exact shape of V-24.
//
// THE FILE IS THE DICTIONARY. These handlers are an editor for
// data/sensitive-words.txt, never a second store: GET reads it, PUT replaces the
// user layer, import merges into it. The injected engine is deliberately not
// consulted here - it reads the same file and picks the change up on its next
// stat (hot reload), so routing the edit through the engine would add a second
// reader of one fact (§3.8). The nail for that is a real turn: after a PUT, a
// message containing the new word comes back masked.
//
// OCTO-FORK: 本地新增路由（`PR-6b2`）— see dev-docs-usdable/需求/20260911/开发计划.md §PR-6b2.
package productruntime

import (
	"net/http"

	"github.com/open-octo/octo-agent/internal/datapath"
	"github.com/open-octo/octo-agent/internal/productclient"
	"github.com/open-octo/octo-agent/internal/sensitive"
)

// codeInvalidWord is the refusal 本地API契约 §2.10 registers for a word that
// normalizes to nothing. It is business-level with the offending word as a
// sibling of the code ({"code": …, "word": …}), and deliberately NOT in
// envelope.go's fieldLevelCodes: that table maps a code onto an input box, and
// this one names an item of a list - there is no box for it (V-75; the same
// reasoning that keeps invalid_value out of the table).
const codeInvalidWord = "invalid_word"

// sensitiveDictPath resolves the dictionary under the data root. The NAME is
// owned by internal/sensitive, so this route and the engine cannot disagree
// about which file is the dictionary; the resolution goes through datapath
// (hard rule 1).
func sensitiveDictPath() (string, error) {
	return datapath.Join(sensitive.DictFileName)
}

// handleSensitiveDictGet implements GET /api/product/sensitive/dict (§2.9): the
// built-in list (read-only, embedded) and the user list (the file).
//
// A missing user file is `[]`, not an error and not a created file: absence
// means "the user layer is empty", and the built-in words still apply because
// the file is additive (§3.9.1, and UserWords' own contract).
func (rt *Runtime) handleSensitiveDictGet(w http.ResponseWriter, r *http.Request) {
	path, err := sensitiveDictPath()
	if err != nil {
		writeCode(w, http.StatusInternalServerError, productclient.CodeInternalError, nil)
		return
	}
	user, err := sensitive.UserWords(path)
	if err != nil {
		// A present-but-invalid file is an error on purpose: reporting an empty
		// list would read as "your words are gone" and invite the user to save
		// over them (see UserWords' doc comment).
		writeCode(w, http.StatusInternalServerError, productclient.CodeInternalError, nil)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"builtin": sensitive.BuiltinWords(),
		"user":    user,
	})
}

// handleSensitiveDictPut implements PUT /api/product/sensitive/dict (§2.10):
// replace the user layer, and write it to disk on the way through.
//
// The answer carries the list that was actually written, which may differ from
// the request: duplicates and words shaped like a built-in entry are dropped.
// That is what makes "the user cannot delete a built-in word" true - deleting
// the row and saving removes it from this list, and the merge puts the built-in
// entry back out of reach (D4).
func (rt *Runtime) handleSensitiveDictPut(w http.ResponseWriter, r *http.Request) {
	var req struct {
		User []string `json:"user"`
	}
	if !decodeBody(w, r, &req) {
		return
	}

	// A word that normalizes to nothing (blank, or only symbols) matches every
	// text, so accepting one would mask whole answers. Refuse the whole write
	// rather than dropping the entry: the user asked for something this build
	// will not store, and silently storing less than asked is how a word list
	// ends up differing from what is on screen.
	for _, word := range req.User {
		if _, ok := sensitive.NormalizeWord(word); !ok {
			writeCode(w, http.StatusBadRequest, codeInvalidWord, map[string]any{"word": word})
			return
		}
	}

	merged, _, _ := sensitive.MergeUserWords(nil, req.User)

	path, err := sensitiveDictPath()
	if err != nil {
		writeCode(w, http.StatusInternalServerError, productclient.CodeInternalError, nil)
		return
	}
	if err := sensitive.WriteUserWords(path, merged); err != nil {
		// No frozen/read-only branch: the freeze belongs to PR-7's watchdog, and
		// until it lands a failed write is simply a failed write (本地API契约
		// §2.10 的一条写明行为). Reporting a business code here would make an
		// infrastructure fault look like a rejected input.
		writeCode(w, http.StatusInternalServerError, productclient.CodeInternalError, nil)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"user": merged})
}

// handleSensitiveDictImport implements POST /api/product/sensitive/dict/import
// (§2.11): merge a batch into the user layer, previewing first when asked.
//
// Import MERGES where PUT replaces - that difference is the whole reason the two
// shapes coexist, so nothing here may fall back to a wholesale overwrite. A word
// that is blank, duplicated, or shaped like a built-in entry is counted as
// skipped rather than refused: an import is a bulk attempt, and failing the lot
// over one unusable line would make pasting a file useless.
func (rt *Runtime) handleSensitiveDictImport(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Words  []string `json:"words"`
		DryRun bool     `json:"dryRun"`
	}
	if !decodeBody(w, r, &req) {
		return
	}

	path, err := sensitiveDictPath()
	if err != nil {
		writeCode(w, http.StatusInternalServerError, productclient.CodeInternalError, nil)
		return
	}
	existing, err := sensitive.UserWords(path)
	if err != nil {
		writeCode(w, http.StatusInternalServerError, productclient.CodeInternalError, nil)
		return
	}

	merged, added, skipped := sensitive.MergeUserWords(existing, req.Words)

	// dryRun is the preview the UI shows before the user confirms, so it must
	// not touch the disk - not even to create the file.
	if !req.DryRun {
		if err := sensitive.WriteUserWords(path, merged); err != nil {
			writeCode(w, http.StatusInternalServerError, productclient.CodeInternalError, nil)
			return
		}
	}

	writeJSON(w, http.StatusOK, map[string]any{"added": added, "skipped": skipped})
}

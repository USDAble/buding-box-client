package server

import (
	"net/http"

	"github.com/open-octo/octo-agent/internal/datapath"
	"github.com/open-octo/octo-agent/internal/sensitive"
)

// Sensitive-word dictionary management (P13). The user dictionary file
// data/sensitive-words.txt is the single source of truth: these handlers are
// just its editor, never a second store. All three routes sit behind the
// product gate (s.apiProduct) like the other account-panel endpoints.
//
// OCTO-FORK: P13 dictionary management — see
// dev-docs-usdable/需求/2260906/技术方案/P13-词库管理界面.md.

// sensitiveDictPath resolves the user dictionary file under the data root.
func sensitiveDictPath() (string, error) {
	return datapath.Join("sensitive-words.txt")
}

// handleSensitiveDictGet serves the built-in (read-only) and user (editable)
// word lists for the management page.
func (s *Server) handleSensitiveDictGet(w http.ResponseWriter, r *http.Request) {
	path, err := sensitiveDictPath()
	if err != nil {
		writeError(w, http.StatusInternalServerError, "resolve dictionary path: "+err.Error())
		return
	}
	user, err := sensitive.UserWords(path)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "read dictionary: "+err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"builtin": sensitive.BuiltinWords(),
		"user":    user,
	})
}

// handleSensitiveDictPut replaces the user dictionary with the given list
// (whole-table PUT, matching the file-is-truth design). A word that is empty
// or pure-symbol after normalization would match every text, so it is refused
// outright (400 invalid_word); duplicates of the built-in list or of another
// entry are dropped so the file stays canonical.
func (s *Server) handleSensitiveDictPut(w http.ResponseWriter, r *http.Request) {
	var req struct {
		User []string `json:"user"`
	}
	if err := readBodyJSON(r, &req); err != nil {
		writeInvalidJSONBody(w, err)
		return
	}

	for _, word := range req.User {
		if _, ok := sensitive.NormalizeWord(word); !ok {
			writeJSON(w, http.StatusBadRequest, map[string]string{"code": "invalid_word", "word": word})
			return
		}
	}
	merged, _, _ := sensitive.MergeUserWords(nil, req.User)

	if datapath.Frozen() {
		writeError(w, http.StatusServiceUnavailable, "dictionary writes are frozen (data root unavailable)")
		return
	}

	path, err := sensitiveDictPath()
	if err != nil {
		writeError(w, http.StatusInternalServerError, "resolve dictionary path: "+err.Error())
		return
	}
	if err := sensitive.WriteUserWords(path, merged); err != nil {
		writeError(w, http.StatusInternalServerError, "write dictionary: "+err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"user": merged})
}

// handleSensitiveDictImport merges a batch of words into the existing user
// dictionary. dryRun returns the preview (added / skipped) without writing;
// the UI shows that preview before confirming the real import.
func (s *Server) handleSensitiveDictImport(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Words  []string `json:"words"`
		DryRun bool     `json:"dryRun"`
	}
	if err := readBodyJSON(r, &req); err != nil {
		writeInvalidJSONBody(w, err)
		return
	}

	path, err := sensitiveDictPath()
	if err != nil {
		writeError(w, http.StatusInternalServerError, "resolve dictionary path: "+err.Error())
		return
	}
	existing, err := sensitive.UserWords(path)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "read dictionary: "+err.Error())
		return
	}
	merged, added, skipped := sensitive.MergeUserWords(existing, req.Words)

	if !req.DryRun {
		if datapath.Frozen() {
			writeError(w, http.StatusServiceUnavailable, "dictionary writes are frozen (data root unavailable)")
			return
		}
		if err := sensitive.WriteUserWords(path, merged); err != nil {
			writeError(w, http.StatusInternalServerError, "write dictionary: "+err.Error())
			return
		}
	}

	writeJSON(w, http.StatusOK, map[string]any{"added": added, "skipped": skipped})
}

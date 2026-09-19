// OCTO-FORK: P13 词库管理（原设计：the dictionary-management boundary）
// — 落地与复用判断见 the current implementation plan §2 `PR-6b2`。
package sensitive

import (
	"os"
	"strings"

	"github.com/open-octo/octo-agent/internal/atomicfile"
)

// DictFileName is the user-extensible word list inside the data root, and this
// constant is its ONE owner (开发规范 §3.8). Two packages need it and they may
// not import each other: internal/server builds the engine from it, and
// internal/productruntime edits the file through the dictionary routes. A
// second literal in either of them would be a second owner of the same fact -
// and the failure it buys is silent: the route would write a file the matcher
// never reads, so every check and masking test stays green while the user's
// new word does nothing.
//
// It stays a NAME rather than a resolved path because resolution is
// internal/datapath's job (hard rule 1): callers do datapath.Join(DictFileName).
const DictFileName = "sensitive-words.txt"

// WriteUserWords replaces the user dictionary file, preserving its leading
// comment block. The caller is responsible for validation (empty / pure-symbol
// words, duplicates); this function only writes.
//
// The write goes through atomicfile.WriteFile, so a drive pulled mid-write
// cannot leave a half-written dictionary behind. That file is the dictionary's
// only store, and the engine re-reads it on ModTime change, so a torn write
// would be read back as the user's word list.
func WriteUserWords(path string, words []string) error {
	var header []string
	if data, err := os.ReadFile(path); err == nil {
		header = leadingCommentBlock(string(data))
	}

	var b strings.Builder
	for _, line := range header {
		b.WriteString(line)
		b.WriteByte('\n')
	}
	for _, w := range words {
		b.WriteString(w)
		b.WriteByte('\n')
	}
	// 0o644, not the 0600 the other stores use: this file is user-authored
	// content, not a secret, and the seed shipped in packaging/portable/data
	// has this mode - a UI edit must not silently change it.
	return atomicfile.WriteFile(path, []byte(b.String()), 0o644)
}

// leadingCommentBlock returns the leading run of comment and blank lines, so
// the user's hand-written explanation at the top of the file survives an edit
// made through the UI. It stops at the first word line; comments below words
// are not preserved (an accepted trade-off, recorded in the local API contract
// §2.10 so it does not read as a defect).
func leadingCommentBlock(content string) []string {
	lines := strings.Split(content, "\n")
	var block []string
	for _, line := range lines {
		trimmed := strings.TrimSpace(line)
		if trimmed == "" || strings.HasPrefix(trimmed, "#") {
			block = append(block, line)
			continue
		}
		break
	}
	return block
}

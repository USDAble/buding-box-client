package sensitive

import (
	_ "embed"
	"errors"
	"os"
	"strings"
	"unicode/utf8"
)

var errInvalidDictionaryUTF8 = errors.New("sensitive: dictionary is not valid UTF-8")

//go:embed default-words.txt
var builtinWordsData []byte

var builtinWords = func() []string {
	words, err := parseUserWords(builtinWordsData)
	if err != nil {
		panic(err)
	}
	return words
}()

func buildDictionary(userWords []string) *dictionary {
	words := make([]string, 0, len(builtinWords)+len(userWords))
	seen := make(map[string]struct{}, cap(words))
	maxLen := 0

	for _, word := range append(append([]string(nil), builtinWords...), userWords...) {
		normalized := normalizeText(word).value
		if normalized == "" {
			continue
		}
		if _, ok := seen[normalized]; ok {
			continue
		}
		seen[normalized] = struct{}{}
		words = append(words, normalized)
		if n := utf8.RuneCountInString(normalized); n > maxLen {
			maxLen = n
		}
	}
	return &dictionary{words: words, maxLen: maxLen}
}

func parseUserWords(data []byte) ([]string, error) {
	if !utf8.Valid(data) {
		return nil, errInvalidDictionaryUTF8
	}
	lines := strings.Split(string(data), "\n")
	words := make([]string, 0, len(lines))
	for _, line := range lines {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		words = append(words, line)
	}
	return words, nil
}

func (e *Engine) dictionarySnapshot() *dictionary {
	e.mu.Lock()
	defer e.mu.Unlock()
	e.refreshLocked()
	return e.dict
}

func (e *Engine) refreshLocked() {
	if e.dictPath == "" {
		e.dict = e.builtin
		e.loaded = true
		e.unreadable = false
		return
	}

	info, err := os.Stat(e.dictPath)
	if err != nil {
		e.useBuiltinLocked(true)
		return
	}
	stamp := fileStamp{modTime: info.ModTime().UnixNano(), size: info.Size()}
	if e.loaded && !e.unreadable && stamp == e.stamp {
		return
	}

	data, err := os.ReadFile(e.dictPath)
	if err != nil {
		e.useBuiltinLocked(true)
		return
	}
	words, err := parseUserWords(data)
	if err != nil {
		e.useBuiltinLocked(true)
		return
	}

	e.dict = buildDictionary(words)
	e.stamp = stamp
	e.loaded = true
	e.unreadable = false
}

func (e *Engine) useBuiltinLocked(unreadable bool) {
	e.dict = e.builtin
	e.stamp = fileStamp{}
	e.loaded = true
	e.unreadable = unreadable
}

// BuiltinWords returns a copy of the read-only built-in dictionary. It is the
// "floor" of the effective dictionary (需求 §5.5): the file only ever adds to
// it, so the dict-management UI (P13) can clear the user list without losing
// the baseline. Callers must not mutate the returned slice.
func BuiltinWords() []string {
	return append([]string(nil), builtinWords...)
}

// UserWords reads the user dictionary file and returns its entries in file
// order, with comment and blank lines stripped. A missing file is an empty
// dictionary, not an error — the built-in words still apply because the file
// is additive. A present-but-invalid file is an error: silently dropping the
// user's words would read as data loss.
func UserWords(path string) ([]string, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return []string{}, nil
		}
		return nil, err
	}
	words, err := parseUserWords(data)
	if err != nil {
		return nil, err
	}
	if words == nil {
		words = []string{}
	}
	return words, nil
}

// NormalizeWord returns the normalized dictionary form of word and whether it
// is usable (non-empty after normalization). The dict-management UI (P13) uses
// it to reject empty/pure-symbol words and to compare candidate words against
// the built-in and existing user words in the same normalized space the
// matcher uses, so "发 票" and "发票" are the same entry.
func NormalizeWord(word string) (string, bool) {
	norm := normalizeText(word).value
	return norm, norm != ""
}

// MergeUserWords merges incoming entries into existing for the import flow.
// Entries are keyed by normalized form, so "发 票" and "发票" collapse to one
// word. Entries that normalize to empty are skipped, as are duplicates of the
// built-in dictionary, an existing word, or an earlier incoming word. It
// returns the merged list (existing order first, then newly added) plus the
// added / skipped counts the import preview renders.
func MergeUserWords(existing, incoming []string) (merged []string, added, skipped int) {
	seen := make(map[string]struct{}, len(builtinWords)+len(existing)+len(incoming))
	for _, w := range builtinWords {
		seen[normalizeText(w).value] = struct{}{}
	}
	merged = make([]string, 0, len(existing)+len(incoming))
	for _, w := range existing {
		norm := normalizeText(w).value
		if norm == "" {
			continue
		}
		if _, dup := seen[norm]; dup {
			continue
		}
		seen[norm] = struct{}{}
		merged = append(merged, w)
	}
	for _, w := range incoming {
		norm := normalizeText(w).value
		if norm == "" {
			skipped++
			continue
		}
		if _, dup := seen[norm]; dup {
			skipped++
			continue
		}
		seen[norm] = struct{}{}
		merged = append(merged, w)
		added++
	}
	return merged, added, skipped
}

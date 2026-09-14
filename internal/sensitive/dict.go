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

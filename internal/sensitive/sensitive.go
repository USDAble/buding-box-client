// Package sensitive implements the product's single compliance-word matcher.
package sensitive

import (
	"sort"
	"strings"
	"sync"
	"unicode/utf8"
)

// Mask is the fixed replacement required by the product specification.
const Mask = "***"

// Hit identifies one merged match in the original UTF-8 text.
type Hit struct {
	Start int
	End   int
	Word  string
}

// Result is the filtered text and its merged original-text ranges.
type Result struct {
	Text string
	Hits []Hit
}

// Matched reports whether the input contained at least one sensitive word.
func (r Result) Matched() bool { return len(r.Hits) > 0 }

type dictionary struct {
	words  []string
	maxLen int
}

type fileStamp struct {
	modTime int64
	size    int64
}

// Engine loads a dictionary and safely serves concurrent filtering calls.
type Engine struct {
	dictPath string
	matcher  matcher
	builtin  *dictionary

	mu         sync.Mutex
	dict       *dictionary
	stamp      fileStamp
	loaded     bool
	unreadable bool
}

type normalizedMatch struct {
	start int
	end   int
	word  string
}

type matcher interface {
	findAll(text normalizedText, words []string) []normalizedMatch
}

type substringMatcher struct{}

func (substringMatcher) findAll(text normalizedText, words []string) []normalizedMatch {
	var matches []normalizedMatch
	for _, word := range words {
		for from := 0; from < len(text.value); {
			rel := strings.Index(text.value[from:], word)
			if rel < 0 {
				break
			}
			startByte := from + rel
			endByte := startByte + len(word)
			start, startOK := text.runeIndexAtByte(startByte)
			end, endOK := text.runeIndexAtByte(endByte)
			if startOK && endOK {
				matches = append(matches, normalizedMatch{
					start: start,
					end:   end,
					word:  word,
				})
			}
			_, size := utf8.DecodeRuneInString(text.value[startByte:])
			from = startByte + size
		}
	}
	return matches
}

// New constructs an engine for dictPath. An empty path explicitly selects the
// built-in dictionary without reporting a file error.
func New(dictPath string) *Engine {
	builtin := buildDictionary(nil)
	e := &Engine{
		dictPath: dictPath,
		matcher:  substringMatcher{},
		builtin:  builtin,
		dict:     builtin,
	}
	e.dictionarySnapshot()
	return e
}

// Filter masks every sensitive substring while preserving all unaffected
// bytes from the original text.
func (e *Engine) Filter(text string) Result {
	return filterWithDictionary(text, e.dictionarySnapshot(), e.matcher)
}

// FileUnreadable reports whether a configured user dictionary is currently
// missing, invalid, or unreadable.
func (e *Engine) FileUnreadable() bool {
	e.mu.Lock()
	defer e.mu.Unlock()
	e.refreshLocked()
	return e.unreadable
}

func filterWithDictionary(text string, dict *dictionary, m matcher) Result {
	if text == "" || len(dict.words) == 0 {
		return Result{Text: text}
	}

	normalized := normalizeText(text)
	if normalized.value == "" {
		return Result{Text: text}
	}
	matches := mergeMatches(m.findAll(normalized, dict.words))
	if len(matches) == 0 {
		return Result{Text: text}
	}

	hits := make([]Hit, 0, len(matches))
	for _, match := range matches {
		hits = append(hits, Hit{
			Start: normalized.spans[match.start].start,
			End:   normalized.spans[match.end-1].end,
			Word:  match.word,
		})
	}

	var b strings.Builder
	b.Grow(len(text))
	last := 0
	for _, hit := range hits {
		b.WriteString(text[last:hit.Start])
		b.WriteString(Mask)
		last = hit.End
	}
	b.WriteString(text[last:])
	return Result{Text: b.String(), Hits: hits}
}

func mergeMatches(matches []normalizedMatch) []normalizedMatch {
	if len(matches) < 2 {
		return matches
	}
	sort.SliceStable(matches, func(i, j int) bool {
		if matches[i].start == matches[j].start {
			return matches[i].end < matches[j].end
		}
		return matches[i].start < matches[j].start
	})

	merged := make([]normalizedMatch, 0, len(matches))
	for _, match := range matches {
		if match.end <= match.start {
			continue
		}
		if len(merged) == 0 || match.start > merged[len(merged)-1].end {
			merged = append(merged, match)
			continue
		}
		if match.end > merged[len(merged)-1].end {
			merged[len(merged)-1].end = match.end
		}
	}
	return merged
}

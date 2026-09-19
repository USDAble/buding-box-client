// Package pii irreversibly masks the built-in personal-information categories
// before user text is persisted or sent to a model. It deliberately returns no
// matched source text or recovery map: callers only receive masked text and
// aggregate category counts.
package pii

import (
	"errors"
	"sort"
	"strings"
	"unicode/utf8"
)

// RuleVersion identifies the exact built-in rule contract used for a result.
const RuleVersion = "builtin-1"

// ErrUnavailable means no usable rule engine was available for a protected operation.
var ErrUnavailable = errors.New("pii: engine is unavailable")

// MatchSummary reports only an aggregate category count, never matched source text.
type MatchSummary struct {
	Category string `json:"category"`
	Count    int    `json:"count"`
}

// Result contains the irreversible masked text and safe aggregate metadata.
type Result struct {
	Masked  string         `json:"masked"`
	Matches []MatchSummary `json:"matches"`
}

// Engine transforms user text without exposing the source matches to callers.
type Engine interface {
	Transform(text string) (Result, error)
}

type builtinEngine struct {
	rules []rule
}

var _ Engine = (*builtinEngine)(nil)

// New returns an engine containing the versioned built-in rule set.
func New() Engine {
	rules := make([]rule, len(builtinRules))
	copy(rules, builtinRules)
	return &builtinEngine{rules: rules}
}

// RuleIDs returns the stable built-in category IDs in contract order.
func RuleIDs() []string {
	ids := make([]string, len(builtinRules))
	for i, r := range builtinRules {
		ids[i] = r.id
	}
	return ids
}

func (e *builtinEngine) Transform(text string) (Result, error) {
	if e == nil || len(e.rules) == 0 {
		return Result{}, ErrUnavailable
	}
	if text == "" {
		return Result{Masked: text}, nil
	}

	scan := newScanText(text)
	protected := placeholderSpans(text)
	var candidates []candidate
	for _, r := range e.rules {
		for _, s := range r.find(scan) {
			if s.start < 0 || s.end <= s.start || s.end > len(text) || overlapsAny(s, protected) {
				continue
			}
			candidates = append(candidates, candidate{span: s, rule: r})
		}
	}
	selected := selectCandidates(candidates)
	if len(selected) == 0 {
		return Result{Masked: text}, nil
	}

	counts := make(map[string]int, len(e.rules))
	var out strings.Builder
	out.Grow(len(text))
	last := 0
	for _, c := range selected {
		out.WriteString(text[last:c.start])
		out.WriteString(c.rule.placeholder)
		last = c.end
		counts[c.rule.id]++
	}
	out.WriteString(text[last:])

	matches := make([]MatchSummary, 0, len(counts))
	for _, r := range e.rules {
		if count := counts[r.id]; count > 0 {
			matches = append(matches, MatchSummary{Category: r.id, Count: count})
		}
	}
	return Result{Masked: out.String(), Matches: matches}, nil
}

type span struct {
	start int
	end   int
}

func (s span) overlaps(other span) bool {
	return s.start < other.end && other.start < s.end
}

func overlapsAny(s span, others []span) bool {
	for _, other := range others {
		if s.overlaps(other) {
			return true
		}
	}
	return false
}

type candidate struct {
	span
	rule rule
}

func selectCandidates(candidates []candidate) []candidate {
	// Resolve overlaps by the documented semantic priority first, then take the
	// longest valid match. Starting position and registry order only break ties,
	// so detection order cannot change the persisted result.
	sort.SliceStable(candidates, func(i, j int) bool {
		a, b := candidates[i], candidates[j]
		if a.rule.priority != b.rule.priority {
			return a.rule.priority > b.rule.priority
		}
		if al, bl := a.end-a.start, b.end-b.start; al != bl {
			return al > bl
		}
		if a.start != b.start {
			return a.start < b.start
		}
		return a.rule.order < b.rule.order
	})
	selected := make([]candidate, 0, len(candidates))
	for _, c := range candidates {
		overlapped := false
		for _, existing := range selected {
			if c.span.overlaps(existing.span) {
				overlapped = true
				break
			}
		}
		if !overlapped {
			selected = append(selected, c)
		}
	}
	sort.Slice(selected, func(i, j int) bool { return selected[i].start < selected[j].start })
	return selected
}

type scanText struct {
	original   string
	normalized string
	// boundaries maps every byte boundary in normalized back to a byte boundary
	// in original. Full-width characters collapse to one ASCII byte while their
	// original three-byte span remains recoverable for replacement.
	boundaries []int
}

func newScanText(text string) scanText {
	var out strings.Builder
	out.Grow(len(text))
	boundaries := []int{0}
	for offset := 0; offset < len(text); {
		r, size := utf8.DecodeRuneInString(text[offset:])
		if r == utf8.RuneError && size == 1 {
			out.WriteByte(text[offset])
			boundaries = append(boundaries, offset+1)
			offset++
			continue
		}
		replacement := normalizeRune(r)
		originalPart := text[offset : offset+size]
		out.WriteString(replacement)
		if replacement == originalPart {
			for i := 1; i <= size; i++ {
				boundaries = append(boundaries, offset+i)
			}
		} else {
			for i := 1; i < len(replacement); i++ {
				boundaries = append(boundaries, offset)
			}
			boundaries = append(boundaries, offset+size)
		}
		offset += size
	}
	return scanText{original: text, normalized: out.String(), boundaries: boundaries}
}

func normalizeRune(r rune) string {
	switch {
	case r >= '０' && r <= '９':
		return string('0' + (r - '０'))
	case r >= 'Ａ' && r <= 'Ｚ':
		return string('A' + (r - 'Ａ'))
	case r >= 'ａ' && r <= 'ｚ':
		return string('a' + (r - 'ａ'))
	}
	switch r {
	case '＠':
		return "@"
	case '．':
		return "."
	case '－', '‐', '‑', '‒', '–', '—':
		return "-"
	case '＿':
		return "_"
	case '：':
		return ":"
	case '／':
		return "/"
	case '＋':
		return "+"
	case '＝':
		return "="
	case '　':
		return " "
	default:
		return string(r)
	}
}

func (s scanText) originalSpan(normalizedStart, normalizedEnd int) span {
	if normalizedStart < 0 || normalizedEnd < normalizedStart || normalizedEnd >= len(s.boundaries) {
		return span{-1, -1}
	}
	return span{s.boundaries[normalizedStart], s.boundaries[normalizedEnd]}
}

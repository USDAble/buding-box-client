// Package pii masks the small set of personal information covered by the
// portable product's privacy mode. P10 deliberately supports phone numbers
// only; broader identifiers belong to a later requirement.
package pii

import (
	"strings"
	"unicode"
	"unicode/utf8"
)

type sourceRune struct {
	value      rune
	start, end int
}

// MaskPhones masks every mainland mobile number in input while preserving the
// user's spaces and hyphens. It is the only PII matcher: callers pass a copy to
// the provider and retain the original for the bubble and session history.
func MaskPhones(input string) string {
	normalized := normalize(input)
	var masks []sourceRune

	for i := 0; i < len(normalized); {
		body, end, ok := phoneAt(normalized, i)
		if !ok {
			i++
			continue
		}
		masks = append(masks, normalized[body+3:body+7]...)
		i = end
	}
	if len(masks) == 0 {
		return input
	}

	var out strings.Builder
	out.Grow(len(input))
	last := 0
	for _, m := range masks {
		out.WriteString(input[last:m.start])
		out.WriteByte('*')
		last = m.end
	}
	out.WriteString(input[last:])
	return out.String()
}

func normalize(input string) []sourceRune {
	out := make([]sourceRune, 0, utf8.RuneCountInString(input))
	for offset, r := range input {
		_, size := utf8.DecodeRuneInString(input[offset:])
		if unicode.IsSpace(r) || r == '-' {
			continue
		}
		out = append(out, sourceRune{value: r, start: offset, end: offset + size})
	}
	return out
}

// phoneAt recognizes one of: 138..., +86138..., or 86138.... i points at
// the body or country prefix in the normalized stream. Numeric boundaries stop
// a valid-looking 11-digit substring from being cut out of a longer number.
func phoneAt(text []sourceRune, i int) (body, end int, ok bool) {
	if i > 0 && isASCIIDigit(text[i-1].value) {
		return 0, 0, false
	}

	body = i
	switch {
	case hasRunes(text, i, '+', '8', '6'):
		body = i + 3
	case hasRunes(text, i, '8', '6'):
		body = i + 2
	}
	end = body + 11
	if end > len(text) || text[body].value != '1' {
		return 0, 0, false
	}
	for j := body; j < end; j++ {
		if !isASCIIDigit(text[j].value) {
			return 0, 0, false
		}
	}
	if end < len(text) && isASCIIDigit(text[end].value) {
		return 0, 0, false
	}
	return body, end, true
}

func hasRunes(text []sourceRune, start int, values ...rune) bool {
	if start+len(values) > len(text) {
		return false
	}
	for i, value := range values {
		if text[start+i].value != value {
			return false
		}
	}
	return true
}

func isASCIIDigit(r rune) bool { return r >= '0' && r <= '9' }

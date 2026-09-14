package sensitive

import (
	"sort"
	"strings"
	"unicode"
	"unicode/utf8"
)

// Keep this list aligned with requirement §5.5. unicode.IsPunct would remove
// more characters than the product contract allows.
const stripSymbols = "!\"#$%&'()*+,-./:;<=>?@[\\]^_`{|}~" +
	"！＂＃＄％＆＇（）＊＋，－．／：；＜＝＞？＠［＼］＾＿｀｛｜｝～" +
	"、。「」『』《》—…·"

type sourceSpan struct {
	start int
	end   int
}

type normalizedText struct {
	value      string
	spans      []sourceSpan
	boundaries []int
}

func normalizeText(input string) normalizedText {
	var b strings.Builder
	b.Grow(len(input))
	spans := make([]sourceSpan, 0, utf8.RuneCountInString(input))
	boundaries := make([]int, 0, cap(spans)+1)

	for offset, r := range input {
		_, size := utf8.DecodeRuneInString(input[offset:])
		r = unicode.ToLower(r)
		if unicode.IsSpace(r) || strings.ContainsRune(stripSymbols, r) {
			continue
		}
		boundaries = append(boundaries, b.Len())
		b.WriteRune(r)
		spans = append(spans, sourceSpan{start: offset, end: offset + size})
	}
	boundaries = append(boundaries, b.Len())

	return normalizedText{
		value:      b.String(),
		spans:      spans,
		boundaries: boundaries,
	}
}

func (n normalizedText) runeIndexAtByte(offset int) (int, bool) {
	i := sort.SearchInts(n.boundaries, offset)
	return i, i < len(n.boundaries) && n.boundaries[i] == offset
}

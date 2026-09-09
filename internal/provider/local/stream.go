package local

// chunks splits the reply text into rune-sized fragments, forcing one
// fragment boundary through the middle of a sensitive word (发票 or gambling)
// so P8's cross-chunk StreamFilter has a reproducible exercise: a full reply
// arrives with its sensitive word delivered across two chunks, and neither
// chunk alone contains the whole word.
//
// It is a pure function so tests can assert the split directly without
// waiting on the stream cadence.
func chunks(text string) []string {
	runes := []rune(text)

	// Locate the first sensitive word present and force a boundary at its
	// midpoint (for a 2-rune word like 发票 that is between the two runes).
	split := -1
	for _, word := range []string{"发票", "gambling", "invoice"} {
		if i := indexRunes(runes, []rune(word)); i >= 0 {
			split = i + len([]rune(word))/2
			break
		}
	}

	const size = 4
	var out []string
	for i := 0; i < len(runes); {
		end := i + size
		// Cut early so the sensitive word straddles this chunk and the next.
		if split > i && split < end {
			end = split
		}
		if end > len(runes) {
			end = len(runes)
		}
		out = append(out, string(runes[i:end]))
		i = end
	}
	return out
}

// indexRunes returns the byte-wise rune index of needle in haystack, or -1.
func indexRunes(haystack, needle []rune) int {
	if len(needle) == 0 || len(needle) > len(haystack) {
		return -1
	}
	for i := 0; i+len(needle) <= len(haystack); i++ {
		match := true
		for j := range needle {
			if haystack[i+j] != needle[j] {
				match = false
				break
			}
		}
		if match {
			return i
		}
	}
	return -1
}

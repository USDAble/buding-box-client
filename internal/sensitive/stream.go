package sensitive

// StreamFilter filters one streamed response while retaining enough source
// text to detect words split across adjacent chunks.
type StreamFilter struct {
	dict    *dictionary
	matcher matcher
	pending string
}

// NewStream snapshots the current dictionary for one streamed response.
func (e *Engine) NewStream() *StreamFilter {
	return &StreamFilter{
		dict:    e.dictionarySnapshot(),
		matcher: e.matcher,
	}
}

// Write appends a chunk and returns the prefix that can no longer participate
// in a match with a future chunk.
func (f *StreamFilter) Write(chunk string) string {
	f.pending += chunk
	if f.pending == "" {
		return ""
	}

	normalized := normalizeText(f.pending)
	if len(normalized.spans) == 0 {
		out := f.pending
		f.pending = ""
		return out
	}

	keep := f.dict.maxLen - 1
	if keep < 0 {
		keep = 0
	}
	if len(normalized.spans) <= keep {
		return ""
	}

	cutRune := len(normalized.spans) - keep
	for {
		moved := false
		for _, match := range mergeMatches(f.matcher.findAll(normalized, f.dict.words)) {
			if match.start < cutRune && match.end > cutRune {
				cutRune = match.start
				moved = true
			}
		}
		if !moved {
			break
		}
	}
	if cutRune == 0 {
		return ""
	}

	cutByte := len(f.pending)
	if cutRune < len(normalized.spans) {
		cutByte = normalized.spans[cutRune].start
	}
	prefix := f.pending[:cutByte]
	f.pending = f.pending[cutByte:]
	return filterWithDictionary(prefix, f.dict, f.matcher).Text
}

// Flush filters and returns all remaining buffered text.
func (f *StreamFilter) Flush() string {
	if f.pending == "" {
		return ""
	}
	out := filterWithDictionary(f.pending, f.dict, f.matcher).Text
	f.pending = ""
	return out
}

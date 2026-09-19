// Package productphone owns the product's phone-number wire format.
package productphone

import "strings"

// Normalize returns the canonical E.164 spelling used across the local and
// control-plane contracts. Mainland numbers may be entered in the legacy local
// form for compatibility; every other number must include its country code.
func Normalize(raw string) (string, bool) {
	var b strings.Builder
	for _, r := range strings.TrimSpace(raw) {
		switch {
		case r >= '0' && r <= '9', r == '+':
			b.WriteRune(r)
		case r == '＋':
			b.WriteByte('+')
		case r >= '０' && r <= '９':
			b.WriteRune('0' + r - '０')
		case r == ' ', r == '-', r == '(', r == ')', r == '.':
			// Presentation separators never reach the platform.
		default:
			return "", false
		}
	}
	v := b.String()
	if len(v) == 11 && v[0] == '1' && v[1] >= '3' && v[1] <= '9' && allDigits(v) {
		v = "+86" + v
	} else if len(v) == 13 && strings.HasPrefix(v, "86") && allDigits(v) && v[2] == '1' && v[3] >= '3' && v[3] <= '9' {
		v = "+" + v
	}
	if len(v) < 9 || len(v) > 16 || v[0] != '+' || v[1] < '1' || v[1] > '9' || !allDigits(v[1:]) {
		return "", false
	}
	return v, true
}

func allDigits(v string) bool {
	for _, r := range v {
		if r < '0' || r > '9' {
			return false
		}
	}
	return true
}

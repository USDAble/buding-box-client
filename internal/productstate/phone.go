package productstate

import (
	"strings"
	"unicode"
)

// NormalizePhone normalizes a user-typed phone number to the canonical 11-digit
// mainland form the state file stores (需求 §5.3.2):
//
//  1. strip all whitespace (including full-width) and hyphens;
//  2. if the result starts with "+86" or "86" AND the remainder is exactly 11
//     digits, drop that prefix;
//  3. the result must be 11 digits starting with "1".
//
// The prefix strip is deliberately conditional on the 11-digit remainder, not
// "helpfully" tolerant: "8613800001" strips to 8 digits, so the prefix stays
// and the whole 10-digit string fails rule 3. Implemented per the requirement
// literally, with no extra guessing.
func NormalizePhone(raw string) (string, bool) {
	var b strings.Builder
	for _, r := range raw {
		if unicode.IsSpace(r) || r == '-' {
			continue
		}
		b.WriteRune(r)
	}
	s := b.String()

	for _, prefix := range []string{"+86", "86"} {
		if strings.HasPrefix(s, prefix) {
			rest := strings.TrimPrefix(s, prefix)
			if len(rest) == 11 && isAllDigits(rest) {
				s = rest
				break
			}
		}
	}

	if len(s) == 11 && s[0] == '1' && isAllDigits(s) {
		return s, true
	}
	return "", false
}

// MaskPhone returns the de-identified 3+4+4 form (138****1234) shown to the
// frontend. The plaintext stays server-side only; the masked copy is all the
// UI ever sees, and the second-login match compares against the bound plaintext
// on the server, never the mask.
func MaskPhone(phone string) string {
	if len(phone) != 11 || !isAllDigits(phone) {
		return phone
	}
	return phone[:3] + "****" + phone[7:]
}

func isAllDigits(s string) bool {
	for i := 0; i < len(s); i++ {
		if s[i] < '0' || s[i] > '9' {
			return false
		}
	}
	return true
}

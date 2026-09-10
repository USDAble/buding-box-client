package productstate

import (
	"errors"
	"regexp"
	"unicode/utf8"
)

// Nickname validation (需求 §5.3.3). The same rules run on the server (here)
// and in the frontend (web/src/lib/nickname.ts) against the same fixtures, so
// the two stay in lockstep — the frontend check is for responsiveness, this
// one is the rule.

// Nickname length: 2–16 characters, counted as runes not bytes.
const (
	nicknameMin = 2
	nicknameMax = 16
)

// Nickname errors, mapped to machine codes in the login handler.
var (
	// ErrNicknameLength is returned when the rune count is outside 2–16.
	ErrNicknameLength = errors.New("nickname must be 2-16 characters")
	// ErrNicknameFormat is returned when a character is outside the allowed set.
	ErrNicknameFormat = errors.New("nickname contains unsupported characters")
)

// nicknameChars matches the allowed set: Han ideographs, letters, digits and
// underscore. Unicode properties rather than [一-龥] so extended Han ranges
// (and Latin/Greek/etc.) aren't wrongly rejected or accepted.
var nicknameChars = regexp.MustCompile(`^[\p{Han}\p{L}\p{Nd}_]+$`)

// ValidateNickname reports whether v is a legal nickname.
func ValidateNickname(v string) error {
	n := utf8.RuneCountInString(v)
	if n < nicknameMin || n > nicknameMax {
		return ErrNicknameLength
	}
	if !nicknameChars.MatchString(v) {
		return ErrNicknameFormat
	}
	return nil
}

// rejectNicknameUntilWired keeps nickname validation fail-closed until the
// server installs the real sensitive-word engine. A nickname hit is refused
// outright, never masked-and-saved.
func rejectNicknameUntilWired(string) bool { return true }

// Sensitive is the sensitive-word gate applied to nicknames before save. The
// server replaces it with the real engine during startup. Keeping the default
// fail-closed prevents a new entry point from silently saving nicknames without
// sensitive-word validation when that wiring is missed.
var Sensitive = rejectNicknameUntilWired

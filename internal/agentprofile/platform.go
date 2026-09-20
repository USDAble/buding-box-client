package agentprofile

import (
	"strconv"
	"strings"
)

// OCTO-FORK: the portable session stores the selected platform publication,
// while its authoritative instructions remain on the platform.
func PlatformReference(id string) (expertID string, version uint32, ok bool) {
	parts := strings.Split(id, ":")
	if len(parts) != 3 || parts[0] != "platform" || len(parts[1]) == 0 || len(parts[1]) > 128 {
		return "", 0, false
	}
	for _, r := range parts[1] {
		if !(r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z' || r >= '0' && r <= '9' || r == '_' || r == '-') {
			return "", 0, false
		}
	}
	n, err := strconv.ParseUint(parts[2], 10, 32)
	if err != nil || n == 0 {
		return "", 0, false
	}
	return parts[1], uint32(n), true
}

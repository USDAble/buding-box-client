package productclient

import "crypto/rand"

// NewClientRequestID returns an opaque correlation id for one call.
//
// It is generated locally and must never be derived from the phone number, the
// install id or a timestamp: the id travels in the Idempotency-Key header, in
// request bodies, and into the platform's ledger, and a predictable one would
// let one client's retry be mistaken for another's (中台交付包 §3.3). 16 random
// bytes is the same shape as a UUIDv4 for the purpose of collision, without
// pulling a UUID dependency for a value the platform treats as opaque.
func NewClientRequestID() string {
	var b [16]byte
	if _, err := rand.Read(b[:]); err != nil {
		// crypto/rand does not fail in practice; a panic here is better than
		// returning a zero id, which would collide across every client and
		// silently defeat the idempotency it exists for.
		panic("productclient: cannot read random bytes: " + err.Error())
	}
	// RFC 4122 version 4 / variant bits, so the value is a valid UUID if the
	// platform's validation ever tightens to one.
	b[6] = (b[6] & 0x0f) | 0x40
	b[8] = (b[8] & 0x3f) | 0x80
	dst := make([]byte, 0, 36)
	const digits = "0123456789abcdef"
	for i, v := range b {
		if i == 4 || i == 6 || i == 8 || i == 10 {
			dst = append(dst, '-')
		}
		dst = append(dst, digits[v>>4], digits[v&0x0f])
	}
	return string(dst)
}

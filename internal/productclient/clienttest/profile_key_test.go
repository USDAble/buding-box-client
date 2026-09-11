//go:build !product_production

package clienttest

import (
	"testing"

	"github.com/open-octo/octo-agent/internal/productprofile"
)

// TestFixtureKeyIsTheDeveloperTrustAnchor pins that the key the stand-in signs
// with is the key a developer build trusts.
//
// The two halves live in files that are edited independently (the seed here,
// the public key in profiles/developer.json), and drift between them fails in a
// way that reads like a broken control plane rather than a mismatched fixture:
// every policy a developer build fetches is rejected, with no hint that the
// anchor is simply the wrong key.
//
// Excluded from product_production builds, which do not carry the fixture key
// at all — trusting it there would be the actual defect.
func TestFixtureKeyIsTheDeveloperTrustAnchor(t *testing.T) {
	profile := productprofile.Current()
	got := profile.TrustedKeyIDs[FixtureSigningKeyID]
	if got != FixtureSigningPublicKey() {
		t.Fatalf("developer profile trusts %q for %q, but the stand-in signs with %q",
			got, FixtureSigningKeyID, FixtureSigningPublicKey())
	}
}

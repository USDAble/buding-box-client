//go:build !product_production && !product_test

// OCTO-FORK: only a testing profile that starts the local stand-in needs its
// fixture key; remote testing profiles carry the testing platform's key.

package clienttest

import (
	"testing"

	"github.com/open-octo/octo-agent/internal/productprofile"
)

// Integration builds trust the actual middle-tier deployment. The publicly
// reproducible fixture seed must remain restricted to isolated test clients.
func TestDeveloperTrustAnchorExcludesFixtureKey(t *testing.T) {
	profile := productprofile.Current()
	if len(profile.TrustedKeyIDs) == 0 {
		t.Fatal("developer integration profile has no signing key")
	}
	for _, key := range profile.TrustedKeyIDs {
		if key == FixtureSigningPublicKey() {
			t.Fatal("integration profile trusts the public fixture signing seed")
		}
	}
}

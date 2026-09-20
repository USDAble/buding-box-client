//go:build !product_production

package clienttest

import (
	"github.com/open-octo/octo-agent/internal/productprofile"
	"testing"
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

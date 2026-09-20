//go:build product_test && !product_production

package main

import (
	"testing"

	"github.com/open-octo/octo-agent/internal/productclient/clienttest"
	"github.com/open-octo/octo-agent/internal/productprofile"
)

func TestLocalTestingProfileTrustsTheBundledStandin(t *testing.T) {
	profile := productprofile.Current()
	if !profile.UsesLocalStandin() {
		return
	}
	if got, want := profile.TrustedKeyIDs[clienttest.FixtureSigningKeyID], clienttest.FixtureSigningPublicKey(); got != want {
		t.Fatalf("local testing profile trusts %q for %q, want bundled stand-in key %q", got, clienttest.FixtureSigningKeyID, want)
	}
}

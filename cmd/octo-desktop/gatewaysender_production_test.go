//go:build product_production

package main

import (
	"testing"

	"github.com/open-octo/octo-agent/internal/productprofile"
	"github.com/open-octo/octo-agent/internal/runtimeport"
)

// The port is only worth anything if the *shipped* build installs it, and the
// shipped build is this binary with this build tag. These tests pin that, rather
// than merely asserting that productSenderFactory works when handed a profile.
//
// Run with: go test -tags product_production ./  (from cmd/octo-desktop)
//
// OCTO-FORK: 宿主 sender 工厂装配（端口 4）— see 01A §3.1.

// TestShippedDesktopInstallsTheSenderFactory is the guarantee that matters for
// B1's acceptance sentence: a production build must install the factory, because
// the install is the only thing that makes OCTO_PROVIDER, the vendor API-key
// variables and data/config.yml inert.
func TestShippedDesktopInstallsTheSenderFactory(t *testing.T) {
	p := productprofile.Current()

	if p.Name != "production" {
		t.Fatalf("shipped build selected profile %q, want production", p.Name)
	}
	if p.AllowEnvironmentModelSource {
		t.Fatal("a shipped build allows the environment as a model source")
	}
	if !p.RequiresControlPlane() {
		t.Fatal("RequiresControlPlane() is false for the shipped profile; the factory would not be installed")
	}
	if productSenderFactory(p) == nil {
		t.Fatal("shipped build installs no SenderFactory: a production session could still be routed by config.yml or OCTO_PROVIDER")
	}
}

// TestShippedDesktopDoesNotTrustAPlaceholderBuild pins the current, deliberate
// state: production.json still carries `.invalid` hosts and an empty trust store,
// so the factory must fail closed. If someone fills production.json in without
// updating this test, that is the signal to replace it with a real readiness
// assertion — the point is that the two cannot drift silently.
func TestShippedDesktopDoesNotTrustAPlaceholderBuild(t *testing.T) {
	p := productprofile.Current()

	if p.ControlPlaneConfigured() && p.HasTrustedKeys() {
		t.Skip("production.json is configured now; replace this test with a positive readiness assertion")
	}

	sender, _, err := productSender{p}.SenderForTurn(t.Context(), runtimeport.SenderRequest{})
	if err != nil {
		t.Fatalf("SenderForTurn err = %v; a readiness problem must not be fatal at startup", err)
	}
	if sender == nil {
		t.Fatal("SenderForTurn returned a nil sender")
	}
	if _, sendErr := sender.SendMessages(t.Context(), "m", "", nil, 0); sendErr == nil {
		t.Fatal("a placeholder build produced a working turn; it must fail closed")
	}
}

package main

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/open-octo/octo-agent/internal/productprofile"
	"github.com/open-octo/octo-agent/internal/runtimeport"
)

// OCTO-FORK: 宿主 sender 工厂装配（端口 4）— see 01A §3.1.

// productionShaped is a Profile with the control plane fully configured, built
// as a literal so these tests do not depend on which profile the build tag
// selected.
func productionShaped() productprofile.Profile {
	return productprofile.Profile{
		SchemaVersion:               1,
		Name:                        "production",
		AllowEnvironmentModelSource: false,
		APIHost:                     "https://api.example.com/v1",
		GatewayHost:                 "https://gateway.example.com/v1",
		TrustedKeyIDs:               map[string]string{"k1": "AAAA"},
	}
}

// TestProductSenderFactoryIsNilForADeveloperProfile is the regression guard for
// the developer workflow: config.yml endpoints, URLs and keys must keep working
// in a developer build, so no factory may be installed there.
func TestProductSenderFactoryIsNilForADeveloperProfile(t *testing.T) {
	dev := productprofile.Profile{Name: "developer", AllowEnvironmentModelSource: true}
	if f := productSenderFactory(dev); f != nil {
		t.Fatal("a developer Profile installed a SenderFactory; it would break config.yml endpoint/URL/key")
	}
}

func TestProductSenderFactoryIsInstalledForAProductionProfile(t *testing.T) {
	if f := productSenderFactory(productionShaped()); f == nil {
		t.Fatal("a production Profile installed no SenderFactory; a production session could still be routed by config.yml")
	}
}

// TestProductSenderNamesTheMissingPiece: the message has to say which piece is
// missing. An unconfigured control plane and an empty trust store are both
// normal in a B0/B1 tree and have different fixes, and "which one is it" is the
// first question anyone reading a bug report will ask.
func TestProductSenderNamesTheMissingPiece(t *testing.T) {
	cases := []struct {
		name       string
		apiHost    string
		gwHost     string
		trustedKey map[string]string
		want       error
	}{
		{
			name:       "no hosts at all",
			trustedKey: map[string]string{"k1": "AAAA"},
			want:       errControlPlaneUnset,
		},
		{
			name:       "shipped .invalid placeholders",
			apiHost:    "https://api.invalid/v1",
			gwHost:     "https://gateway.invalid/v1",
			trustedKey: map[string]string{"k1": "AAAA"},
			want:       errControlPlaneUnset,
		},
		{
			name:       "only one host configured",
			apiHost:    "https://api.example.com/v1",
			trustedKey: map[string]string{"k1": "AAAA"},
			want:       errControlPlaneUnset,
		},
		{
			name:       "hosts configured, trust store empty",
			apiHost:    "https://api.example.com/v1",
			gwHost:     "https://gateway.example.com/v1",
			trustedKey: map[string]string{},
			want:       errNoTrustedKeys,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			f := productSender{productprofile.Profile{
				Name:          "production",
				APIHost:       tc.apiHost,
				GatewayHost:   tc.gwHost,
				TrustedKeyIDs: tc.trustedKey,
			}}
			sender, model, err := f.SenderForTurn(context.Background(), runtimeport.SenderRequest{Model: "m"})

			// The readiness problem must NOT come back as a factory error:
			// server.New treats that as fatal, and a desktop that cannot boot
			// cannot show the page that would explain it.
			if err != nil {
				t.Fatalf("SenderForTurn err = %v; must return a failing sender so the shell can still boot", err)
			}
			if sender == nil {
				t.Fatal("SenderForTurn returned a nil sender")
			}
			if model != "m" {
				t.Fatalf("model = %q, want the requested model passed through", model)
			}
			if _, sendErr := sender.SendMessages(context.Background(), "m", "", nil, 0); !errors.Is(sendErr, tc.want) {
				t.Fatalf("SendMessages err = %v, want %v", sendErr, tc.want)
			}
		})
	}
}

// TestProductSenderReachesTheGatewayGapOnceReady: with the control plane
// configured, the remaining failure is the not-yet-written gateway path, and it
// must be that error specifically — not a silent success, and not a
// config-resolved sender.
func TestProductSenderReachesTheGatewayGapOnceReady(t *testing.T) {
	f := productSender{productionShaped()}
	sender, _, err := f.SenderForTurn(context.Background(), runtimeport.SenderRequest{})
	if err != nil {
		t.Fatalf("SenderForTurn err = %v, want nil", err)
	}
	if _, sendErr := sender.SendMessages(context.Background(), "m", "", nil, 0); !errors.Is(sendErr, errGatewayNotWired) {
		t.Fatalf("SendMessages err = %v, want %v — a configured profile must not silently succeed", sendErr, errGatewayNotWired)
	}
}

// TestProductSenderReadinessErrorIsNotMistakableForOnboarding: the ordinary
// "no API key, complete setup in the Web UI" state also produces a failing turn,
// and the recovery action is completely different (sign in vs fix the build). The
// readiness errors must therefore stay their own values, not a shared message.
func TestProductSenderReadinessErrorIsNotMistakableForOnboarding(t *testing.T) {
	for _, tc := range []struct {
		name string
		err  error
		want string
	}{
		{"no control plane", errControlPlaneUnset, "control-plane"},
		{"no trusted keys", errNoTrustedKeys, "trusted signing keys"},
		{"gateway gap", errGatewayNotWired, "P0-03/P0-05"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if !strings.Contains(tc.err.Error(), tc.want) {
				t.Fatalf("error %q does not mention %q", tc.err, tc.want)
			}
		})
	}
}

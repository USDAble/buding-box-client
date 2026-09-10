package main

import (
	"context"
	"errors"

	"github.com/open-octo/octo-agent/internal/agent"
	"github.com/open-octo/octo-agent/internal/productprofile"
	"github.com/open-octo/octo-agent/internal/runtimeport"
)

// This file is the desktop's half of port 4 (see
// dev-docs-usdable/需求/20260909/P0-正式服务与Agent演进/01A-既有产品逻辑迁出internal-server.md
// §3.1). Installing a SenderFactory is what makes the production bypass inputs
// inert: with it in place the server consults neither OCTO_PROVIDER, the vendor
// API-key environment variables, data/config.yml's default entry, nor a
// session's bound ModelConfig.
//
// OCTO-FORK: 宿主 sender 工厂装配 — see 01A §3.1.

var (
	// errControlPlaneUnset and errNoTrustedKeys are separate errors rather than
	// one "not ready" so the message names the missing piece. `.invalid` hosts
	// and an empty trust store are both expected in a B0/B1 tree (see
	// 01 §1), and "which one is it" is the first question anyone will ask.
	errControlPlaneUnset = errors.New("desktop: profile has no control-plane host configured, so no gateway sender can be built")
	errNoTrustedKeys     = errors.New("desktop: profile has no trusted signing keys, so no signed catalog or policy can be verified")

	// errGatewayNotWired marks the gap between this scaffold and P0-03/P0-05.
	errGatewayNotWired = errors.New("desktop: gateway request path is not implemented yet (P0-03/P0-05)")
)

// productSenderFactory returns the host SenderFactory for this Profile, or nil
// when the Profile is allowed to source models from the environment.
//
// Returning nil for a developer Profile is not a shortcut: installing a factory
// there would break the documented developer workflow (`config.yml` endpoint /
// URL / key). The switch is the Profile predicate, not a build tag, so a
// developer build and a production build cannot disagree about which side of the
// line they are on.
func productSenderFactory(p productprofile.Profile) runtimeport.SenderFactory {
	if !p.RequiresControlPlane() {
		return nil
	}
	return productSender{profile: p}
}

// productSender is the production SenderFactory.
type productSender struct {
	profile productprofile.Profile
}

// SenderForTurn implements runtimeport.SenderFactory.
//
// Readiness problems are reported as a failing sender rather than as an error.
// The distinction matters at startup: server.New treats a factory error as
// fatal, and a desktop that cannot boot cannot show the login or interceptor
// page — which is the only screen that could tell the user what to do. A failing
// sender is equally fail-closed (no turn succeeds) but leaves the shell alive.
func (f productSender) SenderForTurn(_ context.Context, req runtimeport.SenderRequest) (agent.Sender, string, error) {
	if err := f.readinessError(); err != nil {
		return runtimeport.FailingSender(err), req.Model, nil
	}
	// Reached once the profile carries real hosts and keys. The sender itself is
	// P0-03/P0-05: build it from profile.GatewayHost plus the product session
	// token, and construct it per gateway call so the call's clientRequestId
	// rides that call's headers (01 §B1, "每次网关调用构造一个 sender").
	return runtimeport.FailingSender(errGatewayNotWired), req.Model, nil
}

func (f productSender) readinessError() error {
	switch {
	case !f.profile.ControlPlaneConfigured():
		return errControlPlaneUnset
	case !f.profile.HasTrustedKeys():
		return errNoTrustedKeys
	}
	return nil
}

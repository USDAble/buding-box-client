package main

// OCTO-FORK: assembles this fork's product service into the desktop hub. The
// whole product vocabulary for cmd/octo-desktop lives in this file so the diff
// to upstream main.go stays at one line — see
// dev-docs-usdable/需求/20260911/开发计划.md §PR-2b2a

import (
	"log/slog"
	"net/http"
	"runtime"

	"github.com/open-octo/octo-agent/internal/credentialstore"
	"github.com/open-octo/octo-agent/internal/productclient"
	"github.com/open-octo/octo-agent/internal/productprofile"
	"github.com/open-octo/octo-agent/internal/productruntime"
	"github.com/open-octo/octo-agent/internal/productstate"
	"github.com/open-octo/octo-agent/internal/version"
)

// mountProductAPI builds the local product service and returns the hook the
// server calls while registering routes (server.Config.MountAPI).
//
// The assembly lives here rather than in internal/server because the dependency
// runs downhill: this package knows the product, the server does not. That is
// what lets the server stay upstream-shaped, so an upstream merge touches one
// line instead of a product-shaped block.
//
// A nil return is deliberate and bounded (开发规范 §3.9): if the state layer
// cannot be opened, the product routes are simply not registered. The window
// then gets a 404 rather than a fabricated empty state — absence is reported,
// not disguised. The one cause worth naming is a schemaVersion newer than this
// build understands, which E6.2 rule 4 says to refuse rather than migrate.
func mountProductAPI() func(api func(pattern string, h http.HandlerFunc)) {
	state, err := productstate.Open(productstate.Options{
		// The shell is the only component that knows the OS language, so it
		// hands it in once (E6.4 rule 1, PQ18).
		Locale: resolveLang(),
		// The desktop product has no "configure an API key" onboarding; the
		// upstream wizard is unreachable here (A4).
		SuppressOnboarding: true,
	})
	if err != nil {
		slog.Error("product: state unavailable, product routes not mounted", "err", err)
		return nil
	}
	if state.Corrupt() {
		// A damaged file degrades to "not logged in" and is left on disk for
		// recovery (E6.2 rule 5) — the routes stay mounted.
		slog.Warn("product: state file is damaged; continuing as logged out, file preserved")
	}

	creds, err := credentialstore.Open(credentialstore.Options{})
	if err != nil {
		slog.Error("product: credential store unavailable, product routes not mounted", "err", err)
		return nil
	}

	rt := productruntime.New(productruntime.Deps{
		State:    state,
		Creds:    creds,
		Platform: newPlatformClient(state.InstallID()),
	})
	return rt.Mount
}

// newPlatformClient builds the Central Platform client, or returns nil when this
// build names no control plane.
//
// nil is not "no client" to the runtime: it is the signal that produces the
// control_plane_unconfigured error, which is how a developer build without a
// Sandbox host reports itself instead of reaching for a default (A1 rule 6,
// S-6/S-7). Never substitute a fallback host here — degrading to an unconfigured
// source is exactly what the fail-closed rule forbids.
func newPlatformClient(installID string) *productclient.Client {
	profile := productprofile.Current()
	if !profile.ControlPlaneConfigured() {
		slog.Info("product: no control plane configured for this build", "profile", profile.Name)
		return nil
	}
	return productclient.New(profile.APIHost, productclient.ClientMeta{
		Version:   version.Version,
		Platform:  runtime.GOOS,
		Arch:      runtime.GOARCH,
		InstallID: installID,
	}, &productclient.CredentialHolder{})
}

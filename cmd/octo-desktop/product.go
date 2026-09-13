package main

// OCTO-FORK: assembles this fork's product service into the desktop hub. The
// whole product vocabulary for cmd/octo-desktop lives in this file so the diff
// to upstream main.go stays at one line — see
// dev-docs-usdable/需求/20260911/开发计划.md §PR-2b2a

import (
	"crypto/rand"
	"encoding/hex"
	"log/slog"
	"net/http"
	"net/url"
	"runtime"
	"sync"
	"time"

	"github.com/open-octo/octo-agent/internal/agent"
	"github.com/open-octo/octo-agent/internal/app"
	"github.com/open-octo/octo-agent/internal/brand"
	"github.com/open-octo/octo-agent/internal/catalogstore"
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
//
// The window token is generated here, at the top, rather than on first read.
// That ordering is required, not incidental: windowTokenFragment (which
// shellURL consults when it builds the window URL) deliberately reports only an
// already-generated token, so a token born later than the first window show
// would leave that window unable to identify itself. main.go builds the server
// before it shows any window, so generating here is early enough.
// It returns the two seams internal/server needs, both derived from one
// assembly: the local product API's mount hook, and the built-in gateway's
// sender factory (PR-5a). They are returned together, rather than by two
// functions, because they must share one CredentialHolder - the gateway sender
// is built from the token the platform client refreshed, so two holders would
// mean the gateway kept presenting a stale one.
func mountProductAPI() (mount func(api func(pattern string, h http.HandlerFunc)), gatewaySender func(app.ReasoningTuning) (agent.Sender, error)) {
	if windowToken() == "" {
		// Fail closed. Without a token the gate cannot distinguish this window
		// from any other loopback caller, so mounting the routes would publish
		// an unauthenticated API. Not mounting them gives the window a 404,
		// which the frontend already renders as "blocked" — the user is told,
		// and never silently authorized (开发规范 §3.9).
		slog.Error("product: window token unavailable, product routes not mounted")
		return nil, nil
	}

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
		return nil, nil
	}
	if state.Corrupt() {
		// A damaged file degrades to "not logged in" and is left on disk for
		// recovery (E6.2 rule 5) — the routes stay mounted.
		slog.Warn("product: state file is damaged; continuing as logged out, file preserved")
	}

	creds, err := credentialstore.Open(credentialstore.Options{})
	if err != nil {
		slog.Error("product: credential store unavailable, product routes not mounted", "err", err)
		return nil, nil
	}

	// The catalog cache (PR-4b). A failure here does NOT unmount the routes:
	// unlike product-state.json, the cache is not the user's data - it is a
	// re-fetchable copy of something the platform hands out again on the next
	// login. Refusing to serve the product because a cache file would not open
	// would turn a recoverable condition into an outage.
	catalog, err := catalogstore.Open()
	if err != nil {
		slog.Warn("product: catalog cache unavailable; the picker will have nothing to show until the next fetch", "err", err)
	} else if catalog.Corrupt() {
		// Not fatal and not silent: the file is preserved for recovery and the
		// next verified catalog replaces it, but a user whose model list came
		// back empty should have a line in the log to point at (E6.2 rule 5).
		slog.Warn("product: catalog cache is damaged; continuing without it, file preserved")
	}

	// The two facts the blocked page needs before the user types (L-B2). Read
	// here, at assembly time, because the profile is immutable for the life of
	// the process and the judgement belongs to internal/productprofile - the
	// runtime forwards it rather than re-deciding what "configured" means
	// (本地API契约 §2.13).
	profile := productprofile.Current()

	// A restarted process has no access token and no refresh token in memory -
	// the holder starts empty by design (令牌只驻内存，E6 规则 2). Without the
	// credential handed to it, the first authorised call would be refused, the
	// refused refresh would clear the credential (L-A6), and the user would be
	// back on the login screen after every launch (V-32). The holder is built
	// here and used exactly once, right below, so it cannot be handed to the
	// client un-restored by accident.
	tokens := &productclient.CredentialHolder{}
	productruntime.RestoreSession(state, creds, tokens)

	rt := productruntime.New(productruntime.Deps{
		State:    state,
		Creds:    creds,
		Platform: newPlatformClient(state.InstallID(), tokens),
		ControlPlane: productruntime.ControlPlaneStatus{
			Configured:     profile.ControlPlaneConfigured(),
			HasTrustedKeys: profile.HasTrustedKeys(),
		},
		Catalog: catalog,
		// The trust anchor and the audience come from their own owners
		// (productprofile and branding/brand.json) and are handed in as values:
		// verification happens in internal/productclient, so this package must
		// not grow an opinion about which keys count.
		CatalogTrust: productruntime.CatalogTrust{
			TrustedKeys: profile.TrustedKeyIDs,
			Audience:    brand.Load().BrandID,
			Skew:        catalogClockSkew,
		},
	})

	// PR-5a's other half, built from the SAME holder as the platform client
	// above. Sharing it is the point rather than an economy: a refresh makes the
	// new token visible to the gateway sender with no notification step (C2 规则
	// 2), and a second holder would be a second place a restored session has to
	// land - which is exactly the defect V-32 was.
	//
	// The host comes from the profile. It is written bare, and either that shape
	// or one carrying /v1 dials the same path — the provider's endpointURL
	// normalises the suffix (see GatewayEndpoint.Host).
	gateway := productruntime.GatewayEndpoint{Host: profile.GatewayHost, Tokens: tokens}

	return rt.Mount, gateway.Sender
}

// catalogClockSkew tolerates drift between this machine's clock and the
// platform's when a signed catalog's validity window is checked. The window is
// an hour wide, so a couple of minutes is generous without letting a genuinely
// expired catalog through.
const catalogClockSkew = 2 * time.Minute

// newPlatformClient builds the Central Platform client, or returns nil when this
// build names no control plane.
//
// tokens is the holder the client keeps its session in. It is a parameter so the
// restore step has already run by the time the client exists (V-32): the client
// itself never touches a file, which is what keeps it usable in tests.
//
// nil is not "no client" to the runtime: it is the signal that produces the
// control_plane_unconfigured error, which is how a developer build without a
// Sandbox host reports itself instead of reaching for a default (A1 rule 6,
// S-6/S-7). Never substitute a fallback host here — degrading to an unconfigured
// source is exactly what the fail-closed rule forbids.
func newPlatformClient(installID string, tokens *productclient.CredentialHolder) *productclient.Client {
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
	}, tokens)
}

// windowTokenQuery is the URL parameter the shell uses to hand the token to the
// window. Duplicated across the Go/JS boundary (web/src/lib/product.ts:23) and
// pinned by test on both sides, like desktopShellQuery.
const windowTokenQuery = "window_token"

// windowTokenVal is this launch's window token (P3 §3.2). It is generated once
// and lives only in memory: it is never written to the data root, because its
// whole meaning is "this process, this window" and a token on disk would
// outlive the process it identifies.
var (
	windowTokenOnce sync.Once
	windowTokenVal  string
	windowTokenErr  error
)

// windowToken returns this launch's token, generating it on first call.
//
// It must be called before the first window is shown (main.go's server.Config
// does), because windowTokenFragment reads the value without generating it —
// see the note there for why that asymmetry is required.
func windowToken() string {
	windowTokenOnce.Do(func() {
		windowTokenVal, windowTokenErr = newWindowToken()
		if windowTokenErr != nil {
			slog.Error("product: window token unavailable", "err", windowTokenErr)
		}
	})
	return windowTokenVal
}

// windowTokenFragment returns the query fragment shellURL appends so the window
// can identify itself to the product gate, or "" when there is no token.
//
// It deliberately does NOT generate a token, and that is the whole reason it
// exists separately from windowToken:
//
//   - Upstream's TestShellURL asserts the exact string shellURL produces, and it
//     runs in this package. A fragment that generated on read would make that
//     test's URL grow a token and fail — i.e. the upstream contract would break
//     from a purely local change.
//   - The empty case is also a real runtime case: `octo serve` has no window, so
//     its URL must stay exactly upstream's.
//
// Generation therefore happens once, early, in main (via server.Config), and
// this function only reports the result.
func windowTokenFragment() string {
	if windowTokenVal == "" {
		return ""
	}
	return "&" + windowTokenQuery + "=" + url.QueryEscape(windowTokenVal)
}

// newWindowToken returns a fresh window token: 32 random bytes, hex-encoded.
// A per-launch random value is what makes the gate an identity check rather
// than a shared secret that has to be provisioned or rotated.
func newWindowToken() (string, error) {
	var b [32]byte
	if _, err := rand.Read(b[:]); err != nil {
		return "", err
	}
	return hex.EncodeToString(b[:]), nil
}

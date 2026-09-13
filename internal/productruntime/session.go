package productruntime

import (
	"context"
	"errors"
	"log/slog"

	"github.com/open-octo/octo-agent/internal/credentialstore"
	"github.com/open-octo/octo-agent/internal/productclient"
	"github.com/open-octo/octo-agent/internal/productstate"
)

// RestoreSession puts the refresh token from data/credential.json into tokens, so
// a process that has just started can reach the platform without asking for an
// SMS code again (需求基线 E6, L-E2).
//
// It belongs in this package, next to the code that ends a session, because the
// two are one fact seen twice: handleLogout and the refused-refresh path (L-A6)
// delete the credential, and a restart must not undo a session that was ended.
// The holder is a parameter rather than something this builds because the
// client that owns it is constructed by whoever knows the build's profile; this
// function only moves a value from one owner to the other.
//
// It never fails. Absence of a session is a normal state, and a credential file
// that cannot be read is the login screen's problem, not a reason to refuse to
// start: the window is expected to come up either way (E6 规则 5).
func RestoreSession(state *productstate.Store, creds *credentialstore.Store, tokens *productclient.CredentialHolder) {
	if tokens == nil {
		// A caller with no holder has nothing to restore into. This is a
		// wiring mistake, but it is not worth taking the process down for.
		return
	}
	if creds == nil {
		return
	}

	cred, present, err := creds.Load()
	if err != nil {
		// The store drops a damaged file rather than failing, so reaching here
		// means something else went wrong. Logged, not fatal: the user will be
		// asked to sign in again, which is a recovery.
		slog.Warn("product: credential unreadable; starting signed out", "err", err)
		return
	}

	// E7: a logout ends the session. The credential file is deleted when that
	// happens, and if the delete failed (a read-only stick is exactly the case
	// this product ships onto) the file outlives the logout. Restoring it would
	// sign the user back in after they asked to leave, so the state file decides
	// — and the leftover credential is reported rather than deleted, because
	// startup must not write user-editable files (§3.9.1).
	if state != nil && !state.State().LoggedIn {
		if present {
			slog.Warn("product: a credential outlived its logout; ignoring it (the next sign-in replaces it)")
		}
		return
	}

	if !present {
		// Nothing on disk. Not worth a warning on its own, but when the state
		// already claims a session this is the one line that explains why the
		// first authorised call will land the user back on the login screen.
		if state != nil && state.State().LoggedIn {
			slog.Info("product: no credential on disk; the first platform call will ask for a sign-in")
		}
		return
	}

	// Only the refresh token. The access token is never on disk (E6 规则 2), so
	// the first call runs without a bearer and trades this token for one — which
	// is the rotation the platform expects, not an extra round trip to avoid.
	tokens.Set(productclient.Credentials{RefreshToken: cred.RefreshToken})
}

// SessionRenewer obtains an access token for a restored session and keeps the
// rotated refresh token on disk.
//
// WHY THE TWO HALVES ARE ONE TYPE (PR-4c1). The platform rotates the refresh
// token on every exchange and stops honouring the old one (中台交付包 §5.2 第 3
// 条), so an exchange that is not written down spends the credential on disk and
// leaves the next launch holding a value the platform has already discarded
// (V-42). Doing only the exchange looks like it works — the launch in progress
// chats normally — and the damage lands on the *following* launch, as a forced
// SMS login. That is the worst shape a defect can have, so the two are wired as
// one object and not offered separately.
//
// It is the ask GatewayEndpoint.Ensure makes, which is why it lives here next to
// RestoreSession: both exist to serve the same fact — a restart has a refresh
// token and no access token.
type SessionRenewer struct {
	// Platform owns the exchange, including the single-flight and the rotation
	// safety (internal/productclient). nil means this build names no control
	// plane, which is refused rather than worked around.
	Platform *productclient.Client
	// Tokens is the holder the exchange lands in — the same one the platform
	// client and the gateway sender share.
	Tokens *productclient.CredentialHolder
	// Creds owns data/credential.json. nil means the rotation is not persisted;
	// that is a wiring mistake for an assembled build and is accepted here only
	// so a test can model the half-fix V-42 describes.
	Creds *credentialstore.Store
	// State owns the login flag and the install id (E6.1). It is read for the
	// two descriptive fields when the file has to be rebuilt, and written when a
	// refusal ends the session.
	State *productstate.Store
}

// Ensure makes sure an access token is held, exchanging the refresh token when
// there is none, and writes the rotation.
//
// A refusal ends the session locally, through the same function the platform
// funnel uses, because the turn path does not pass through that funnel: the
// error here becomes a refused turn, not an HTTP answer the frontend maps. Two
// copies of "the session is over" would let one of them drift (开发规范 §3.8),
// and the drift would show as a login flag that outlives the credential.
//
// A failure to WRITE does not fail the turn. The token is in memory and this
// launch works; what it costs is the next launch, and the log says so. Refusing
// the turn instead would turn "the stick is write-protected" into "the product
// does not work" (§3.9 names the fallback, tells the user, and names the
// recovery — here: a writable root, or signing in again).
func (r SessionRenewer) Ensure(ctx context.Context) error {
	if r.Platform == nil {
		return errors.New("this build names no control plane, so no session token can be obtained")
	}
	if err := r.Platform.EnsureToken(ctx); err != nil {
		if IsSessionExpired(err) {
			forgetSession(r.Creds, r.State, err)
		}
		return err
	}
	if err := r.persistRotation(); err != nil {
		slog.Warn("product: the rotated credential could not be written; this launch keeps working and the next one will ask for a sign-in",
			"err", err)
	}
	return nil
}

// persistRotation writes the refresh token the exchange left in memory.
//
// The file is used as the base and only the token is replaced, so the fields
// that are not this operation's business — the masked number for an offline
// start, the install id mirrored for diagnostics (E6.3) — are not recomputed
// from second sources and cannot drift. The one exception is a file that is
// absent or was dropped as damaged: then the two descriptive fields are filled
// from their owner (internal/productstate) rather than left blank, because a
// session that survives is worth more than a diagnostic line.
//
// Nothing is written when the two already agree — which is the case for every
// turn after the first, and is what keeps a read-only data root from logging a
// failure per turn.
func (r SessionRenewer) persistRotation() error {
	if r.Creds == nil || r.Tokens == nil {
		return nil
	}
	current := r.Tokens.Get().RefreshToken
	if current == "" {
		// Nothing to record. The holder has an access token but no refresh token
		// (only reachable through a wiring mistake), and writing a blank
		// credential would delete a session rather than save one.
		return nil
	}

	cred, present, err := r.Creds.Load()
	if err != nil {
		return err
	}
	if present && cred.RefreshToken == current {
		return nil
	}
	if !present {
		cred = credentialstore.Credential{}
		if r.State != nil {
			cred.InstallID = r.State.InstallID()
			if account := r.State.State().Account; account != nil {
				cred.AccountPhoneMasked = account.PhoneMasked
			}
		}
	}
	cred.RefreshToken = current
	// Left empty on purpose: obtainedAt answers "when was the token in this file
	// obtained", and this one was obtained now — the store stamps it (E6.3).
	cred.ObtainedAt = ""
	return r.Creds.Save(cred)
}

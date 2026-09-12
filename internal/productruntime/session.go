package productruntime

import (
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

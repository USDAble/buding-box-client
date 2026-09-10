// Package credentialstore is the seam P0-08 fills with an OS-backed secret
// store. It exists now so that the packages which need credentials can be
// written against an interface instead of each inventing its own file.
//
// Three rules are part of the contract, not implementation detail:
//
//   - Only the refresh token is persisted. The access token is short-lived and
//     stays in memory, so a stolen data directory yields nothing usable.
//   - It must never be backed by product-state.json or config.yml. Those are
//     plain, user-readable, user-editable files; a token there is a token the
//     user can copy and the support flow can accidentally paste into a log.
//   - No read path exists for the UI. Presenting a token is never a feature, so
//     the only outputs are "there is a session" and "here is the refresh token
//     for exactly one call".
package credentialstore

import (
	"context"
	"errors"
)

// ErrNotFound means no credential is stored, which is the normal first-run
// state. It is distinct from a read failure: "no session" routes to the login
// screen, while a store that cannot be read must surface as an error rather
// than silently logging the user out.
var ErrNotFound = errors.New("credentialstore: no stored credential")

// Tokens is what the store holds. RefreshToken is the only value with an
// on-disk representation; the account identity that goes with it belongs to
// product-state (display) or the bootstrap response (authority), not here.
type Tokens struct {
	RefreshToken string
}

// Store persists the refresh token across restarts.
//
// Implementations must treat Save as a replace and Clear as idempotent: logout
// runs on a path where the credential may already be gone, and failing there
// would strand the user on a screen they asked to leave.
type Store interface {
	// Load returns ErrNotFound when nothing is stored.
	Load(ctx context.Context) (Tokens, error)
	// Save replaces the stored token.
	Save(ctx context.Context, tokens Tokens) error
	// Clear removes the stored token. Clearing an empty store succeeds.
	Clear(ctx context.Context) error
}

// Package credentialstore owns data/credential.json, the one long-lived
// credential: the refresh token (需求基线 E6.3).
//
// It is a separate file from product-state.json on purpose. The state file is
// copied into diagnostic bundles and is safe to read; this one must never be.
// The access token has no place here at all - it lives only in memory (E6 规则 2).
//
// The file is stored in plaintext and is meant to travel with the drive: the
// product is a portable installation, and copying data/ to another machine must
// keep the session working (E6 规则 3, D-004 defers the hardening).
package credentialstore

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"sync"
	"time"

	"github.com/open-octo/octo-agent/internal/atomicfile"
	"github.com/open-octo/octo-agent/internal/datapath"
)

// CurrentSchemaVersion is the credential structure version this build writes.
const CurrentSchemaVersion = 1

const credentialFile = "credential.json"

// Credential is the on-disk shape, one field per row of the E6.3 table.
type Credential struct {
	SchemaVersion int    `json:"schemaVersion"`
	RefreshToken  string `json:"refreshToken"`
	ObtainedAt    string `json:"obtainedAt"`
	// AccountPhoneMasked lets an offline start say who was last signed in
	// without storing the number itself.
	AccountPhoneMasked string `json:"accountPhoneMasked,omitempty"`
	// InstallID mirrors the state file's value so a diagnostic bundle can line
	// the two up.
	InstallID string `json:"installId,omitempty"`
}

// Store reads and writes the credential file. It is safe for concurrent use.
type Store struct {
	mu   sync.Mutex
	path string

	now func() time.Time
}

// Options configure Open.
type Options struct {
	// Now overrides the clock, for tests.
	Now func() time.Time
}

// Open prepares the store. It does not read or create the file: a missing or
// unreadable credential is simply "not logged in", and opening the store must
// never be the thing that decides the user's phase.
func Open(opts Options) (*Store, error) {
	path, err := datapath.Join(credentialFile)
	if err != nil {
		return nil, fmt.Errorf("credentialstore: resolve path: %w", err)
	}
	now := opts.Now
	if now == nil {
		now = time.Now
	}
	return &Store{path: path, now: now}, nil
}

// Path reports the file's location, for diagnostics.
func (s *Store) Path() string { return s.path }

// Load returns the stored credential.
//
// A missing file means "not logged in" and is not an error. A file that cannot
// be parsed is deleted and reported as "not logged in" as well: unlike the state
// file, a corrupt credential has nothing worth preserving, and leaving it in
// place would pin the user to the login screen forever (E6.3 规则 4).
func (s *Store) Load() (Credential, bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.load()
}

func (s *Store) load() (Credential, bool, error) {
	raw, err := os.ReadFile(s.path)
	if errors.Is(err, os.ErrNotExist) {
		return Credential{}, false, nil
	}
	if err != nil {
		return Credential{}, false, fmt.Errorf("credentialstore: read %s: %w", s.path, err)
	}

	var cred Credential
	if err := json.Unmarshal(raw, &cred); err != nil || cred.RefreshToken == "" {
		// Written by something other than this build, or truncated by a drive
		// pulled mid-write. Either way it cannot sign anyone in.
		_ = os.Remove(s.path)
		return Credential{}, false, nil
	}
	if cred.SchemaVersion > CurrentSchemaVersion {
		// Written by a newer build. Its token is still usable, but the fields
		// around it are not understood, so treat it as unusable rather than
		// guess - and do not delete it, because a newer build may come back.
		return Credential{}, false, fmt.Errorf(
			"credentialstore: credential is from a newer version (%d > %d)",
			cred.SchemaVersion, CurrentSchemaVersion)
	}
	return cred, true, nil
}

// Save writes the credential atomically.
func (s *Store) Save(cred Credential) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	cred.SchemaVersion = CurrentSchemaVersion
	if cred.ObtainedAt == "" {
		cred.ObtainedAt = s.now().UTC().Format(time.RFC3339)
	}
	return s.write(cred)
}

func (s *Store) write(cred Credential) error {
	buf, err := json.MarshalIndent(cred, "", "  ")
	if err != nil {
		return fmt.Errorf("credentialstore: encode: %w", err)
	}
	buf = append(buf, '\n')
	return atomicfile.WriteFile(s.path, buf, 0o600)
}

// Delete removes the credential file. Logging out is exactly this (E7).
// Removing a file that is not there is not an error.
func (s *Store) Delete() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := os.Remove(s.path); err != nil && !errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("credentialstore: delete %s: %w", s.path, err)
	}
	return nil
}

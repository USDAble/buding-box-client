// Package productstate owns data/product-state.json, the non-sensitive state of
// one installation (需求基线 E6.1).
//
// It never holds a token: credentials live in data/credential.json, owned by
// internal/credentialstore. That split is deliberate - a diagnostic bundle or a
// copy of the state file must never carry a usable credential.
//
// Every path goes through internal/datapath; no path is resolved here.
package productstate

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"sync"
	"time"

	"github.com/open-octo/octo-agent/internal/atomicfile"
	"github.com/open-octo/octo-agent/internal/datapath"
)

// CurrentSchemaVersion is the structure version this build writes. It only ever
// increases (E6.2 规则 1).
const CurrentSchemaVersion = 1

// stateFile is the file name under the data root.
const stateFile = "product-state.json"

// DefaultLocale is used when the desktop shell supplies none. The shell is the
// one that knows the system language (PQ18); this is only a fallback.
const DefaultLocale = "en"

// ErrIncompatibleVersion means the file on disk was written by a newer build.
// Parsing it with the current structures would drop fields the user's data still
// has, so the caller must refuse rather than downgrade (E6.2 规则 4).
var ErrIncompatibleVersion = errors.New("productstate: state file is from a newer version")

// ErrCorrupt means the file exists but could not be parsed. The file is left
// untouched on disk; the caller degrades to "not logged in" and sends the user
// back to the login screen (E6.2 规则 5).
var ErrCorrupt = errors.New("productstate: state file is corrupt")

// Activation is the activation record as the UI needs it. BoxCode is stored and
// rendered verbatim, never masked (需求基线 E5).
type Activation struct {
	ActivatedAt string `json:"activatedAt"`
	ExpiresAt   string `json:"expiresAt"`
	// BoxCode is absent for records written before the field existed. Absence is
	// "unregistered", not damage - the UI shows a dash (E6.2 规则 2, PQ19).
	BoxCode string `json:"boxCode,omitempty"`
}

// Account is the identity summary. Only the masked phone number is kept: the
// plaintext number has no local use (E6.1).
type Account struct {
	PhoneMasked string `json:"phoneMasked"`
	Nickname    string `json:"nickname"`
	LastLoginAt string `json:"lastLoginAt"`
}

// Credits is a read-only projection of the platform ledger. The client never
// computes credits locally (E9).
type Credits struct {
	Balance   int64  `json:"balance"`
	MonthUsed int64  `json:"monthUsed"`
	MonthKey  string `json:"monthKey"`
}

// Plan is the subscription name reported by the platform.
type Plan struct {
	Name string `json:"name"`
}

// Prefs are the user's local choices.
type Prefs struct {
	Locale              string `json:"locale"`
	InputSensitiveCheck bool   `json:"inputSensitiveCheck"`
	DefaultChatMode     string `json:"defaultChatMode"`
}

// State is the on-disk shape, one field per row of the E6.1 table.
type State struct {
	SchemaVersion      int         `json:"schemaVersion"`
	LoggedIn           bool        `json:"loggedIn"`
	Activated          bool        `json:"activated"`
	Activation         *Activation `json:"activation,omitempty"`
	Account            *Account    `json:"account,omitempty"`
	Credits            Credits     `json:"credits"`
	Plan               Plan        `json:"plan"`
	Prefs              Prefs       `json:"prefs"`
	SuppressOnboarding bool        `json:"suppressOnboarding"`
	// InstallID identifies this installation, not this account: it is generated
	// once and never changes on login, logout or account switch (E6.1, N-1). It
	// is sent to the platform as installId; it is not a hardware serial.
	InstallID string `json:"installId"`
}

// PublicState is what the local API hands to the web UI (本地API契约 §1.3).
//
// InstallID is deliberately absent: the UI has no use for it, and the fewer
// places it appears the fewer can leak it into a diagnostic bundle.
type PublicState struct {
	SchemaVersion      int         `json:"schemaVersion"`
	LoggedIn           bool        `json:"loggedIn"`
	Activated          bool        `json:"activated"`
	Activation         *Activation `json:"activation"`
	Account            *Account    `json:"account"`
	Credits            Credits     `json:"credits"`
	Plan               Plan        `json:"plan"`
	Prefs              Prefs       `json:"prefs"`
	SuppressOnboarding bool        `json:"suppressOnboarding"`
}

// LoginOutcome is what a successful platform login contributes to local state.
//
// It is a plain struct rather than the transport DTO so this package stays
// independent of the platform contract: the HTTP layer maps one to the other,
// which keeps a contract rename from reaching the storage format.
type LoginOutcome struct {
	PhoneMasked string
	Nickname    string
	ActivatedAt string
	ExpiresAt   string
	BoxCode     string
}

// Options configure Open.
type Options struct {
	// Locale seeds prefs.locale on first run. Empty falls back to
	// DefaultLocale. It is the shell that knows the system language.
	Locale string
	// SuppressOnboarding is set by the desktop build to skip the upstream
	// "configure an API key" wizard (A4).
	SuppressOnboarding bool
	// Now overrides the clock, for tests.
	Now func() time.Time
}

// Store is the in-memory view of the state file. It is safe for concurrent use.
type Store struct {
	mu    sync.Mutex
	path  string
	state State
	// corrupt records that the file on disk could not be parsed and was left
	// alone. It is surfaced so the caller can report it instead of pretending
	// the installation is simply fresh.
	corrupt bool

	now func() time.Time
}

// Open loads the state file, creating it with defaults if it is absent.
//
// Creating the file on first access is the one startup write the norms permit
// (开发规范 §3.9.1): it is idempotent, it happens only when the file is missing,
// and it does not block startup - a failure to write degrades to in-memory
// defaults rather than stopping the process.
//
// An existing file is never rewritten here. In particular a corrupt file is
// preserved as-is, because a parse bug must not be able to destroy the user's
// data (E6.2 规则 3/5).
func Open(opts Options) (*Store, error) {
	path, err := datapath.Join(stateFile)
	if err != nil {
		return nil, fmt.Errorf("productstate: resolve path: %w", err)
	}
	now := opts.Now
	if now == nil {
		now = time.Now
	}
	s := &Store{path: path, now: now}

	raw, err := os.ReadFile(path)
	switch {
	case errors.Is(err, os.ErrNotExist):
		s.state = s.seed(opts)
		if werr := s.write(); werr != nil {
			// The drive may be read-only. Continuing with the in-memory seed lets
			// the app render; the user is told when an action actually needs to
			// persist something.
			return s, werr
		}
		return s, nil
	case err != nil:
		return nil, fmt.Errorf("productstate: read %s: %w", path, err)
	}

	var state State
	if err := json.Unmarshal(raw, &state); err != nil {
		s.state = s.seed(opts)
		s.corrupt = true
		return s, ErrCorrupt
	}
	if state.SchemaVersion > CurrentSchemaVersion {
		return nil, fmt.Errorf("%w: file has %d, this build understands %d",
			ErrIncompatibleVersion, state.SchemaVersion, CurrentSchemaVersion)
	}
	// A file without a version is treated as the only version that has ever
	// existed. Missing fields are "unregistered", never a reason to refuse
	// (E6.2 规则 2).
	if state.SchemaVersion == 0 {
		state.SchemaVersion = CurrentSchemaVersion
	}
	if state.InstallID == "" {
		// Older data predating installId. It is an installation property, so it
		// is generated here and takes effect on the next save rather than
		// triggering a write now.
		state.InstallID = newInstallID()
	}
	if state.Prefs.Locale == "" {
		state.Prefs.Locale = localeOrDefault(opts.Locale)
	}
	s.state = state
	return s, nil
}

// State returns a copy of the current state.
func (s *Store) State() State {
	s.mu.Lock()
	defer s.mu.Unlock()
	return clone(s.state)
}

// Corrupt reports whether the file on disk was unparseable and preserved.
func (s *Store) Corrupt() bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.corrupt
}

// InstallID is the installation identifier sent to the platform.
func (s *Store) InstallID() string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.state.InstallID
}

// PublicState projects the state for the local API.
func (s *Store) PublicState() PublicState {
	s.mu.Lock()
	defer s.mu.Unlock()
	return publicOf(s.state)
}

// SetSuppressOnboarding records the desktop build's skip-the-wizard flag.
func (s *Store) SetSuppressOnboarding(v bool) error {
	return s.mutate(func(st *State) { st.SuppressOnboarding = v })
}

// SetLocale stores the interface language. It works while logged out, because
// the login screen itself has to be readable (E8 规则 5).
func (s *Store) SetLocale(locale string) error {
	return s.mutate(func(st *State) { st.Prefs.Locale = locale })
}

// SetPrefs stores the caller's preference changes. A nil pointer leaves that
// preference alone.
func (s *Store) SetPrefs(locale *string, inputSensitiveCheck *bool, defaultChatMode *string) error {
	return s.mutate(func(st *State) {
		if locale != nil {
			st.Prefs.Locale = *locale
		}
		if inputSensitiveCheck != nil {
			st.Prefs.InputSensitiveCheck = *inputSensitiveCheck
		}
		if defaultChatMode != nil {
			st.Prefs.DefaultChatMode = *defaultChatMode
		}
	})
}

// ApplyLogin records a successful login: the account, the activation record and
// the balance projection.
//
// The activation fields come from the platform, not from the form the user
// filled in. That is what lets a second installation - a fresh data directory -
// learn the box code it never had typed into it (E6.1).
func (s *Store) ApplyLogin(out LoginOutcome, credits Credits) error {
	return s.mutate(func(st *State) {
		st.LoggedIn = true
		st.Activated = true
		st.Account = &Account{
			PhoneMasked: out.PhoneMasked,
			Nickname:    out.Nickname,
			LastLoginAt: s.now().UTC().Format(time.RFC3339),
		}
		st.Activation = &Activation{
			ActivatedAt: out.ActivatedAt,
			ExpiresAt:   out.ExpiresAt,
			BoxCode:     out.BoxCode,
		}
		st.Credits = credits
	})
}

// SetNickname updates the nickname only.
func (s *Store) SetNickname(nickname string) error {
	return s.mutate(func(st *State) {
		if st.Account == nil {
			st.Account = &Account{}
		}
		st.Account.Nickname = nickname
	})
}

// SetCredits replaces the balance projection.
func (s *Store) SetCredits(credits Credits) error {
	return s.mutate(func(st *State) { st.Credits = credits })
}

// Logout clears the login flag only.
//
// The activation record and the bound phone number stay: logging out is not
// un-activating, and the second-login form needs the number to compare against
// (E7, E2). Deleting the credential file is the caller's job - that is what
// actually ends the session.
func (s *Store) Logout() error {
	return s.mutate(func(st *State) { st.LoggedIn = false })
}

// WithdrawActivationClaim lowers the local activation flag after the platform
// refused a sign-in that offered no activation credential (V-44).
//
// WHY THE LOCAL FLAG FOLLOWS. `activated` answers "is this installation
// authorized", and the platform is the only owner of that answer (E1 rule 6:
// the record lives server-side). What this file holds is the last answer it
// gave, so when it says "no" the copy has to move or the two disagree - and the
// disagreement is not cosmetic: the blocked page renders the five-field
// activation form only for `activated: false` (BlockedView.svelte:34/276), so a
// stale `true` leaves the user reading "incorrect activation code" above a form
// with no activation-code field, with no way back except editing this file from
// outside the product. The same principle as V-22's fix: the wall's shape comes
// from the server's answer, not from a value the client makes up.
//
// WHAT SURVIVES, AND WHY. The account and the activation record stay: they are
// what the platform last said, E7 keeps them across a session ending, the
// second-sign-in form compares against the masked number, and the licence page
// shows the box code. Dropping them would lose the only local memory of an
// authorization whose code can never be re-submitted (E1 rule 2) - and the next
// successful activation overwrites both anyway.
//
// It is deliberately separate from Logout: a refused sign-in is not a session
// ending, and the two must not be written as one (开发规范 §3.8).
func (s *Store) WithdrawActivationClaim() error {
	return s.mutate(func(st *State) { st.Activated = false })
}

// mutate applies fn under the lock and persists the result.
func (s *Store) mutate(fn func(*State)) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	fn(&s.state)
	return s.write()
}

// write persists the current state. The caller holds the lock.
func (s *Store) write() error {
	buf, err := json.MarshalIndent(s.state, "", "  ")
	if err != nil {
		return fmt.Errorf("productstate: encode: %w", err)
	}
	buf = append(buf, '\n')
	if err := atomicfile.WriteFile(s.path, buf, 0o600); err != nil {
		return err
	}
	s.corrupt = false
	return nil
}

// seed builds the first-run state. InstallID is generated here, once, and is
// never regenerated - not on login, not on logout, not on an account switch
// (E6.1, N-1).
func (s *Store) seed(opts Options) State {
	return State{
		SchemaVersion:      CurrentSchemaVersion,
		InstallID:          newInstallID(),
		Prefs:              Prefs{Locale: localeOrDefault(opts.Locale)},
		SuppressOnboarding: opts.SuppressOnboarding,
	}
}

func localeOrDefault(locale string) string {
	if locale == "" {
		return DefaultLocale
	}
	return locale
}

// publicOf projects the stored state for the UI. The one normalisation is the
// invariant that a logged-in installation is always activated (本地API契约 §1.3).
func publicOf(st State) PublicState {
	return PublicState{
		SchemaVersion:      st.SchemaVersion,
		LoggedIn:           st.LoggedIn,
		Activated:          st.Activated || st.LoggedIn,
		Activation:         st.Activation,
		Account:            st.Account,
		Credits:            st.Credits,
		Plan:               st.Plan,
		Prefs:              st.Prefs,
		SuppressOnboarding: st.SuppressOnboarding,
	}
}

// clone deep-copies the pointer fields so a caller cannot mutate the store
// through the returned value.
func clone(st State) State {
	out := st
	if st.Activation != nil {
		a := *st.Activation
		out.Activation = &a
	}
	if st.Account != nil {
		a := *st.Account
		out.Account = &a
	}
	return out
}

// newInstallID returns a random UUIDv4. crypto/rand is used rather than the
// time or the hostname: this identifier must not be predictable, and it must not
// be derived from the machine it runs on (E1 规则 5 - no hardware serials).
func newInstallID() string {
	var b [16]byte
	if _, err := rand.Read(b[:]); err != nil {
		// crypto/rand does not fail in practice; a zero identifier would be
		// worse than an obviously malformed one, so encode the error path as
		// all-zero bytes and let a duplicate show up in diagnostics.
		return "00000000-0000-4000-8000-000000000000"
	}
	b[6] = (b[6] & 0x0f) | 0x40 // version 4
	b[8] = (b[8] & 0x3f) | 0x80 // variant 10
	h := hex.EncodeToString(b[:])
	return h[0:8] + "-" + h[8:12] + "-" + h[12:16] + "-" + h[16:20] + "-" + h[20:32]
}

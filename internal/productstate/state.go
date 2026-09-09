// Package productstate holds the single runtime state file the portable
// product persists under data/product-state.json. Every piece of fake product
// data — login, activation, credits, switches, the default chat mode — lives
// in this one file, so later PRs (P6 credits, P8 sensitive words, P9 chat
// modes) fill in fields rather than inventing new state files.
//
// The schema is fixed here once (P3), not grown per PR: see
// dev-docs-usdable/需求/2260906/技术方案/P3-登录态与产品门.md §3.4.
package productstate

import (
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/open-octo/octo-agent/internal/datapath"
)

// State is the full on-disk document. Account and Activation are pointers so
// "not logged in" and "not activated" are distinguishable from zero values;
// Credits, Plan and Prefs are values so they always round-trip with defaults.
type State struct {
	SchemaVersion int         `json:"schemaVersion"`
	Account       *Account    `json:"account"`
	Activation    *Activation `json:"activation"`
	Credits       Credits     `json:"credits"`
	Plan          Plan        `json:"plan"`
	Prefs         Prefs       `json:"prefs"`
}

// Account is the login state. Phone stores the normalized 11-digit plaintext —
// local fake data in the user's own data root, and the second-login check must
// compare against the bound number, which a masked copy cannot do.
type Account struct {
	Phone       string    `json:"phone"`
	PhoneMasked string    `json:"phoneMasked"`
	Nickname    string    `json:"nickname"`
	Token       string    `json:"token"`
	LastLoginAt time.Time `json:"lastLoginAt"`
}

// Activation records the one-time activation. ExpiresAt is informational only
// this phase — expiry is not enforced (需求 §5.3.5).
type Activation struct {
	Activated   bool      `json:"activated"`
	Code        string    `json:"code"`
	ActivatedAt time.Time `json:"activatedAt"`
	ExpiresAt   time.Time `json:"expiresAt"`
}

// Credits holds the fake points balance (P6 fills it; schema only here).
type Credits struct {
	Balance   int    `json:"balance"`
	MonthUsed int    `json:"monthUsed"`
	MonthKey  string `json:"monthKey"`
}

// Plan holds the package/plan name (P5 surfaces it; schema only here).
type Plan struct {
	Name string `json:"name"`
}

// Prefs holds user preferences that must survive a logout (P8/P9 fill them).
type Prefs struct {
	Locale              string `json:"locale"`
	InputSensitiveCheck bool   `json:"inputSensitiveCheck"`
	DefaultChatMode     string `json:"defaultChatMode"`
}

// clone returns a deep copy so a caller holding a Snapshot can never mutate
// the store's in-memory state through a shared *Account pointer.
func (s State) clone() State {
	if s.Account != nil {
		a := *s.Account
		s.Account = &a
	}
	if s.Activation != nil {
		a := *s.Activation
		s.Activation = &a
	}
	return s
}

// Store is a goroutine-safe in-memory copy of the state file. Reads are
// served from memory (U盘 reads are slow); writes re-marshal and atomically
// rename over the file so a concurrent reader can never observe a torn file.
type Store struct {
	mu    sync.RWMutex
	path  string
	state State
}

// Open loads the state file, returning a Store over the zero State when the
// file is missing (first run). A present-but-unparseable file (pulled during a
// write) is reported as an error so the caller can surface it, but the Store
// is still returned over the zero State — the caller decides whether to warn
// rather than the store silently "resetting" an activation that may only be
// unreadable right now.
func Open(path string) (*Store, error) {
	s := &Store{path: path}
	b, err := os.ReadFile(path)
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return s, nil
		}
		return s, fmt.Errorf("productstate: read %s: %w", path, err)
	}
	if len(b) == 0 {
		return s, nil
	}
	if err := json.Unmarshal(b, &s.state); err != nil {
		return s, fmt.Errorf("productstate: parse %s: %w", path, err)
	}
	return s, nil
}

// Snapshot returns a deep copy of the current state. The caller does not hold
// the lock and can read the returned value freely.
func (s *Store) Snapshot() State {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.state.clone()
}

// Mutate applies fn to a copy of the state and, on success, atomically
// persists the result before publishing it. A nil fn error leaves the on-disk
// and in-memory state untouched. Writes are refused while the data root is
// frozen (a pulled U盘): a write must not recreate an empty data/ or land on
// the host.
func (s *Store) Mutate(fn func(*State) error) error {
	if datapath.Frozen() {
		return datapath.ErrFrozen
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	next := s.state.clone()
	if err := fn(&next); err != nil {
		return err
	}
	b, err := json.MarshalIndent(next, "", "  ")
	if err != nil {
		return fmt.Errorf("productstate: marshal: %w", err)
	}
	b = append(b, '\n')
	if err := os.MkdirAll(filepath.Dir(s.path), 0o755); err != nil {
		return fmt.Errorf("productstate: create dir for %s: %w", s.path, err)
	}
	tmp := s.path + ".tmp"
	if err := os.WriteFile(tmp, b, 0o600); err != nil {
		return fmt.Errorf("productstate: write %s: %w", tmp, err)
	}
	if err := os.Rename(tmp, s.path); err != nil {
		return fmt.Errorf("productstate: replace %s: %w", s.path, err)
	}
	s.state = next
	return nil
}

// LoggedIn reports whether an account is present with a live token.
func (s *Store) LoggedIn() bool {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.state.LoggedIn()
}

// LoggedIn reports whether a login token is live. A logged-out account keeps
// its binding (phone/nickname) so the second-login form can prefill and
// compare against them (需求 §5.3.4), so an account's presence alone is not
// "logged in" — the token is what distinguishes it (需求 §5.3.5: logout clears
// the token, not the binding).
func (s State) LoggedIn() bool {
	return s.Account != nil && s.Account.Token != ""
}

// Activated reports whether the product has been activated.
func (s *Store) Activated() bool {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.state.Activated()
}

// Activated reports whether this state snapshot is activated.
func (s State) Activated() bool {
	return s.Activation != nil && s.Activation.Activated
}

// PublicState is the de-identified view of State handed to the frontend: the
// account's phone plaintext and token are stripped, since neither is needed
// client-side (the token is a server-side-only fake). The activation record's
// code stays server-side too; only the timestamps go out, so the account panel
// can render "active · N days left" / "expired" (需求 §5.4.2, P5).
type PublicState struct {
	SchemaVersion int               `json:"schemaVersion"`
	LoggedIn      bool              `json:"loggedIn"`
	Activated     bool              `json:"activated"`
	Activation    *PublicActivation `json:"activation,omitempty"`
	Account       *PublicAcct       `json:"account,omitempty"`
	Credits       Credits           `json:"credits"`
	Plan          Plan              `json:"plan"`
	Prefs         Prefs             `json:"prefs"`
	// SuppressOnboarding tells the desktop shell to never enter the upstream
	// first-run "configure API key" wizard, even with an empty model list
	// (需求 §5.1.2-3「禁止出现首次配 Key 向导」). It is NOT part of the
	// persisted state — it is a server-runtime flag the handler stamps on, true
	// only when the server runs in the desktop shell (window token present).
	// OCTO-FORK: P9 — see dev-docs-usdable/需求/2260906/技术方案/P9-模式与模型.md §3.5.
	SuppressOnboarding bool `json:"suppressOnboarding"`
}

// PublicActivation is the activation timestamps the UI may see. Activated (the
// bool) is still exposed separately for P4's login-form branching.
type PublicActivation struct {
	ActivatedAt time.Time `json:"activatedAt"`
	ExpiresAt   time.Time `json:"expiresAt"`
}

// PublicAcct is the account fields the UI may see.
type PublicAcct struct {
	PhoneMasked string    `json:"phoneMasked"`
	Nickname    string    `json:"nickname"`
	LastLoginAt time.Time `json:"lastLoginAt"`
}

// Public returns the de-identified state for GET /api/product/state.
func (s State) Public() PublicState {
	p := PublicState{
		SchemaVersion: s.SchemaVersion,
		LoggedIn:      s.LoggedIn(),
		Activated:     s.Activation != nil && s.Activation.Activated,
		Credits:       s.Credits,
		Plan:          s.Plan,
		Prefs:         s.Prefs,
	}
	if s.Activation != nil && s.Activation.Activated {
		p.Activation = &PublicActivation{
			ActivatedAt: s.Activation.ActivatedAt,
			ExpiresAt:   s.Activation.ExpiresAt,
		}
	}
	if s.Account != nil {
		p.Account = &PublicAcct{
			PhoneMasked: s.Account.PhoneMasked,
			Nickname:    s.Account.Nickname,
			LastLoginAt: s.Account.LastLoginAt,
		}
	}
	return p
}

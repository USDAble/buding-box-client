package hooks

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"os"
	"path/filepath"
	"sync"

	"github.com/open-octo/octo-agent/internal/datapath"
)

// Project-level hooks.yml is loaded from a repo the user may have just cloned,
// so its shell commands are untrusted by default: a malicious repo could run
// arbitrary commands (or, once PreToolUse is configured, auto-approve dangerous
// tools) the moment octo starts in that directory. Trust-on-first-use gates
// this — the first time a project hooks.yml (or a changed one) is seen, the
// user is asked once; the approved content's fingerprint is remembered so it
// never re-prompts until the file changes. The user-level data/hooks.yml is
// the user's own and needs no such gate.

// ProjectConfigPath returns <cwd>/.octo/hooks.yml, the project-level hooks file
// a repo may carry, at the same path upstream uses.
//
// The path is spelled out rather than renamed because upstream's shipped
// documentation (docs/src/content/docs/guides/hooks.md) and the runtime skill
// doc (internal/skills/defaults/product-help/HOOKS.md) both name it — a renamed
// directory would make both wrong while fixing nothing. See
// the portable data-root boundary §3 and PQ31.
func ProjectConfigPath(cwd string) string {
	if cwd == "" {
		return ""
	}
	// The project's own directory in the user's repo — not the data root (<exe dir>/data).
	return filepath.Join(cwd, ".octo", "hooks.yml") // octo-literal-allow: project-level path in the user's repo, not the data root
}

// Fingerprint is the content hash a trust decision is keyed on, so editing a
// trusted project hooks.yml re-triggers the prompt.
func Fingerprint(content []byte) string {
	sum := sha256.Sum256(content)
	return hex.EncodeToString(sum[:])
}

// trustStoreMu serialises read-modify-write of the on-disk trust store within
// this process. Cross-process races are benign (last writer wins; a lost record
// just re-prompts once).
var trustStoreMu sync.Mutex

// trustStorePath returns data/hooks-trust.json, or "" when the data root is
// unavailable.
// OCTO-FORK: the portable product keeps hook trust next to the executable, not
// in the host home — see the portable data-root boundary.
func trustStorePath() string {
	p, err := datapath.Join("hooks-trust.json")
	if err != nil {
		return ""
	}
	return p
}

func loadTrustStore() map[string]string {
	m := map[string]string{}
	p := trustStorePath()
	if p == "" {
		return m
	}
	b, err := os.ReadFile(p)
	if err != nil {
		return m
	}
	_ = json.Unmarshal(b, &m) // a corrupt store degrades to "nothing trusted"
	return m
}

// IsTrusted reports whether path's current fingerprint has been approved.
func IsTrusted(path, fingerprint string) bool {
	trustStoreMu.Lock()
	defer trustStoreMu.Unlock()
	return loadTrustStore()[path] == fingerprint
}

// RecordTrust persists approval of path at the given fingerprint. Best-effort:
// a write failure just means the user is asked again next time.
func RecordTrust(path, fingerprint string) error {
	trustStoreMu.Lock()
	defer trustStoreMu.Unlock()
	p := trustStorePath()
	if p == "" {
		return nil
	}
	m := loadTrustStore()
	m[path] = fingerprint
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		return err
	}
	b, err := json.MarshalIndent(m, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(p, b, 0o600)
}

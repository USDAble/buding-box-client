// Package productprofile exposes the immutable runtime profile compiled into a
// desktop binary. It deliberately does not read data/config.yml, environment
// variables, command-line flags, or the data root: none of those user-writable
// sources may turn a standard production package into a developer package.
package productprofile

import (
	"encoding/json"
	"fmt"
	"sync"
)

const (
	Production = "production"
	Developer  = "developer"
)

// Startup records whether an existing runtime capability is available to the
// desktop build. It is not a visibility or authorization policy: P0 keeps the
// existing channel, tool, MCP, and background implementations intact, while
// productpolicy later governs UI visibility and PEP decisions.
type Startup struct {
	Channels        bool `json:"channels"`
	Tools           bool `json:"tools"`
	MCP             bool `json:"mcp"`
	BackgroundTasks bool `json:"backgroundTasks"`
}

// Profile is the immutable, build-selected product runtime configuration.
// Dynamic control-plane data intentionally does not belong here.
type Profile struct {
	SchemaVersion               int     `json:"schemaVersion"`
	Name                        string  `json:"name"`
	AllowDevWebview             bool    `json:"allowDevWebview"`
	AllowEnvironmentModelSource bool    `json:"allowEnvironmentModelSource"`
	AllowDataRootOverride       bool    `json:"allowDataRootOverride"`
	Startup                     Startup `json:"startup"`
}

var (
	currentOnce sync.Once
	current     Profile
)

// Current returns the profile selected by the build tag. A malformed embedded
// asset is a release-build error, so fail closed rather than guessing a mode.
func Current() Profile {
	currentOnce.Do(func() {
		if err := json.Unmarshal([]byte(embeddedProfileJSON), &current); err != nil {
			panic(fmt.Sprintf("product profile: parse embedded configuration: %v", err))
		}
		if err := current.Validate(); err != nil {
			panic(fmt.Sprintf("product profile: invalid embedded configuration: %v", err))
		}
	})
	return current
}

// IsProduction reports whether this binary is a standard production package.
func (p Profile) IsProduction() bool { return p.Name == Production }

// Validate rejects ambiguous or unsafe profile assets before they can affect a
// desktop launch.
func (p Profile) Validate() error {
	if p.SchemaVersion != 1 {
		return fmt.Errorf("unsupported schemaVersion %d", p.SchemaVersion)
	}
	switch p.Name {
	case Production:
		if p.AllowDevWebview || p.AllowEnvironmentModelSource || p.AllowDataRootOverride {
			return fmt.Errorf("production profile enables a developer source")
		}
	case Developer:
		// Developer is intentionally explicit in its JSON; individual false
		// values are valid when a development workflow does not need a source.
	default:
		return fmt.Errorf("unknown profile %q", p.Name)
	}
	return nil
}

// Package productprofile exposes the immutable runtime profile compiled into a
// desktop binary. It deliberately does not read data/config.yml, environment
// variables, command-line flags, or the data root: none of those user-writable
// sources may turn a standard production package into a developer package.
package productprofile

import (
	"crypto/ed25519"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net/url"
	"strings"
	"sync"
)

const (
	Production = "production"
	Developer  = "developer"
)

// Placeholder control-plane hosts for a release that has not been given the
// deployment host yet. They use the RFC 6761 reserved `.invalid` TLD, so DNS
// can never resolve them: an unconfigured package fails loudly instead of
// sending product traffic somewhere unintended. Replace both (and populate
// TrustedKeyIDs) before shipping — the packaging preflight reports them.
const (
	UnsetAPIHost     = "https://api.invalid/v1"
	UnsetGatewayHost = "https://gateway.invalid/v1"
)

// GatewayEndpointID is the id of the built-in gateway endpoint (需求基线 C1).
//
// It lives here, next to GatewayHost, because the two name the same thing from
// two sides: the id is the half that appears in a session's composite model id
// and the host is the half a turn dials. Neither is build-varying - unlike the
// profile's other fields, this constant must be identical in the developer and
// production profiles, because a session written by one build has to resolve in
// the other. That is also why it is a constant rather than a Deps field: there
// is nothing for a test to vary, and a field would only create a way to wire it
// up wrong.
//
// The value is a fixed ASCII data key, not the English brand name: it is
// persisted inside session files and compared against config.yml's endpoints, so
// a rebrand must not move it (开发规范 §3.1 规则 2, the annotated `buding-*`
// exception for data keys).
//
// It must not collide with an id a user can create in config.yml. C1 规则 3 says
// a same-named config.yml endpoint must not override the built-in one, and the
// upstream resolution path (config.EntryByModel) scans user endpoints first —
// so the name is deliberately specific rather than the bare `gateway`.
//
// Its shape is provisional until C1 lands the endpoint itself (PR-5): nothing
// can bind to a composite id yet, so the value is still free to change. Once a
// session has been written with it, it is not.
const GatewayEndpointID = "buding-gateway"

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
//
// APIHost / GatewayHost / TrustedKeyIDs are the one exception to "no remote
// data in the profile": bootstrap has to know where to call before it can ask
// anything, so the control-plane address cannot be delivered by the control
// plane. They are therefore compile-time constants, exactly like the profile
// itself, and never come from config.yml, the environment, product-state.json,
// a cache or WebView input. See P0-01 §「中台请求客户端的实现形态」§1.
type Profile struct {
	SchemaVersion               int               `json:"schemaVersion"`
	Name                        string            `json:"name"`
	AllowDevWebview             bool              `json:"allowDevWebview"`
	AllowEnvironmentModelSource bool              `json:"allowEnvironmentModelSource"`
	AllowDataRootOverride       bool              `json:"allowDataRootOverride"`
	APIHost                     string            `json:"apiHost"`
	GatewayHost                 string            `json:"gatewayHost"`
	TrustedKeyIDs               map[string]string `json:"trustedKeyIDs"`
	Startup                     Startup           `json:"startup"`
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

// ControlPlaneConfigured reports whether the profile names a real control
// plane. A release ships with `.invalid` placeholders until the deployment
// host is known, so this is false for an unconfigured build. Callers must
// treat false as "refuse to serve", never as "fall back to a default host".
func (p Profile) ControlPlaneConfigured() bool {
	return !isUnsetHost(p.APIHost) && !isUnsetHost(p.GatewayHost)
}

// HasTrustedKeys reports whether any signing key is trusted. False means no
// signed policy can be verified, which is equivalent to having no catalog
// (P0-01 §1). It is never a reason to accept an unverified source.
func (p Profile) HasTrustedKeys() bool { return len(p.TrustedKeyIDs) > 0 }

// RequiresControlPlane reports whether this Profile's model traffic must come
// from the product control plane — the gateway and the signed catalog — rather
// than from the environment or data/config.yml.
//
// A developer Profile may source models from the environment (that is what
// allowEnvironmentModelSource is for), so it does not. Anything that must not be
// overridable by ambient configuration asks this instead of comparing
// Profile.Name against a string, so the two profiles cannot drift apart from the
// predicate that reads them.
func (p Profile) RequiresControlPlane() bool { return !p.AllowEnvironmentModelSource }

// isUnsetHost reports whether host is absent or a `.invalid` placeholder.
func isUnsetHost(host string) bool {
	u, err := url.Parse(strings.TrimSpace(host))
	if err != nil || u.Hostname() == "" {
		return true
	}
	return strings.HasSuffix(u.Hostname(), ".invalid")
}

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
		// Required, and https-only: an absent host must stop the launch rather
		// than fall back to a default, localhost or config.yml, and the
		// control-plane transport never speaks plaintext (P0-01 §1, §2).
		if err := validateHost("apiHost", p.APIHost, true, true); err != nil {
			return err
		}
		if err := validateHost("gatewayHost", p.GatewayHost, true, true); err != nil {
			return err
		}
	case Developer:
		// Developer is intentionally explicit in its JSON; individual false
		// values are valid when a development workflow does not need a source.
		// A local sandbox may be plain http, so the scheme is not pinned here
		// (scope rule, 开发规范 §3.10).
		if err := validateHost("apiHost", p.APIHost, false, false); err != nil {
			return err
		}
		if err := validateHost("gatewayHost", p.GatewayHost, false, false); err != nil {
			return err
		}
	default:
		return fmt.Errorf("unknown profile %q", p.Name)
	}
	return validateTrustedKeyIDs(p.TrustedKeyIDs)
}

// validateHost checks a compile-time control-plane host. required is true when
// an absent value must fail the launch instead of degrading to a default.
func validateHost(field, value string, required, requireHTTPS bool) error {
	value = strings.TrimSpace(value)
	if value == "" {
		if required {
			return fmt.Errorf("%s must not be empty", field)
		}
		return nil
	}
	u, err := url.Parse(value)
	if err != nil {
		return fmt.Errorf("%s is not a valid URL: %w", field, err)
	}
	if !u.IsAbs() || u.Host == "" {
		return fmt.Errorf("%s must be an absolute URL, got %q", field, value)
	}
	if u.User != nil {
		return fmt.Errorf("%s must not carry userinfo", field)
	}
	switch u.Scheme {
	case "https":
	case "http":
		if requireHTTPS {
			return fmt.Errorf("%s must use https, got %q", field, value)
		}
	default:
		return fmt.Errorf("%s scheme must be http or https, got %q", field, u.Scheme)
	}
	return nil
}

// validateTrustedKeyIDs checks the signature trust store. An empty map is
// structurally valid — it means no signed policy can be verified, which the
// policy layer must treat as "no catalog" rather than as "unverified is fine".
func validateTrustedKeyIDs(keys map[string]string) error {
	for id, key := range keys {
		if strings.TrimSpace(id) == "" {
			return fmt.Errorf("trustedKeyIDs has an empty keyId")
		}
		raw, err := base64.StdEncoding.DecodeString(strings.TrimSpace(key))
		if err != nil {
			return fmt.Errorf("trustedKeyIDs[%q] is not valid base64: %w", id, err)
		}
		if len(raw) != ed25519.PublicKeySize {
			return fmt.Errorf("trustedKeyIDs[%q] must decode to %d bytes, got %d",
				id, ed25519.PublicKeySize, len(raw))
		}
	}
	return nil
}

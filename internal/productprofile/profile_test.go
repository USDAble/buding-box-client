package productprofile

import (
	"encoding/base64"
	"encoding/json"
	"os"
	"strings"
	"testing"
)

func TestCurrentIsValidBuildSelectedProfile(t *testing.T) {
	p := Current()
	if err := p.Validate(); err != nil {
		t.Fatalf("Current().Validate() = %v", err)
	}
}

func TestCurrentKeepsExistingRuntimeCapabilities(t *testing.T) {
	p := Current()
	if !p.Startup.Channels || !p.Startup.Tools || !p.Startup.MCP || !p.Startup.BackgroundTasks {
		t.Fatalf("Profile(%q) disables an existing runtime capability: %+v", p.Name, p.Startup)
	}
}

func TestProductionCannotEnableDesktopDeveloperInputs(t *testing.T) {
	p := Profile{SchemaVersion: 1, Name: Production, AllowDevWebview: true}
	if err := p.Validate(); err == nil {
		t.Fatal("production profile with a development input was accepted")
	}
}

func TestDeveloperProfileMayBeRestrictive(t *testing.T) {
	p := Profile{SchemaVersion: 1, Name: Developer}
	if err := p.Validate(); err != nil {
		t.Fatalf("restrictive developer profile rejected: %v", err)
	}
}

// ed25519Key returns a base64 key of the given raw length, so a test can build
// both a well-formed trust entry and a wrong-length one.
func ed25519Key(n int) string {
	return base64.StdEncoding.EncodeToString(make([]byte, n))
}

func TestProductionRequiresControlPlaneHosts(t *testing.T) {
	// An absent host must fail the launch rather than degrade to a default,
	// localhost or config.yml (P0-01 §1).
	for _, tc := range []struct {
		name string
		p    Profile
	}{
		{"both absent", Profile{SchemaVersion: 1, Name: Production}},
		{"apiHost absent", Profile{SchemaVersion: 1, Name: Production, GatewayHost: "https://gw.example.com/v1"}},
		{"gatewayHost absent", Profile{SchemaVersion: 1, Name: Production, APIHost: "https://api.example.com/v1"}},
		{"apiHost blank", Profile{SchemaVersion: 1, Name: Production, APIHost: "   ", GatewayHost: "https://gw.example.com/v1"}},
	} {
		if err := tc.p.Validate(); err == nil {
			t.Errorf("production profile with %s was accepted", tc.name)
		}
	}
}

func TestProductionRequiresHTTPSControlPlane(t *testing.T) {
	p := Profile{
		SchemaVersion: 1, Name: Production,
		APIHost: "http://api.example.com/v1", GatewayHost: "https://gw.example.com/v1",
	}
	if err := p.Validate(); err == nil {
		t.Fatal("production profile accepted a plaintext apiHost")
	}
}

func TestControlPlaneHostMustBeAbsoluteAndCredentialFree(t *testing.T) {
	for _, tc := range []struct {
		name string
		host string
	}{
		{"relative", "/v1"},
		{"no host", "https:///v1"},
		{"userinfo", "https://user:pass@api.example.com/v1"},
		{"unsupported scheme", "ftp://api.example.com/v1"},
	} {
		p := Profile{
			SchemaVersion: 1, Name: Production,
			APIHost: tc.host, GatewayHost: "https://gw.example.com/v1",
		}
		if err := p.Validate(); err == nil {
			t.Errorf("apiHost %s (%q) was accepted", tc.name, tc.host)
		}
	}
}

func TestDeveloperMayLeaveControlPlaneUnset(t *testing.T) {
	// A developer sandbox may point at a local plaintext host, or at nothing.
	for _, host := range []string{"", "http://127.0.0.1:8080/v1"} {
		p := Profile{SchemaVersion: 1, Name: Developer, APIHost: host}
		if err := p.Validate(); err != nil {
			t.Errorf("developer apiHost %q rejected: %v", host, err)
		}
	}
}

func TestTrustedKeyIDsMustBeEd25519PublicKeys(t *testing.T) {
	base := func(keys map[string]string) Profile {
		return Profile{
			SchemaVersion: 1, Name: Production,
			APIHost: "https://api.example.com/v1", GatewayHost: "https://gw.example.com/v1",
			TrustedKeyIDs: keys,
		}
	}
	if err := base(nil).Validate(); err != nil {
		t.Fatalf("no trusted keys must stay structurally valid (it means no catalog): %v", err)
	}
	if err := base(map[string]string{"policy-2026-a": ed25519Key(32)}).Validate(); err != nil {
		t.Fatalf("well-formed trust entry rejected: %v", err)
	}
	for _, tc := range []struct {
		name string
		keys map[string]string
	}{
		{"empty keyId", map[string]string{"  ": ed25519Key(32)}},
		{"not base64", map[string]string{"policy-2026-a": "not base64!!"}},
		{"wrong length", map[string]string{"policy-2026-a": ed25519Key(16)}},
	} {
		if err := base(tc.keys).Validate(); err == nil {
			t.Errorf("trustedKeyIDs with %s was accepted", tc.name)
		}
	}
}

func TestUnsetControlPlaneIsNotTreatedAsConfigured(t *testing.T) {
	// The checked-in release asset carries `.invalid` placeholders until the
	// deployment host is known. They must pass Validate (the shape is right)
	// but must never be reported as a usable control plane.
	p := Profile{
		SchemaVersion: 1, Name: Production,
		APIHost: UnsetAPIHost, GatewayHost: UnsetGatewayHost,
	}
	if err := p.Validate(); err != nil {
		t.Fatalf("placeholder hosts must stay structurally valid: %v", err)
	}
	if p.ControlPlaneConfigured() {
		t.Fatal("placeholder hosts were reported as a configured control plane")
	}
	if p.HasTrustedKeys() {
		t.Fatal("empty trust store reported as trusted keys")
	}
}

func TestIsUnsetHost(t *testing.T) {
	for _, host := range []string{"", "   ", UnsetAPIHost, "https://anything.invalid/v1", "not a url"} {
		if !isUnsetHost(host) {
			t.Errorf("isUnsetHost(%q) = false, want true", host)
		}
	}
	for _, host := range []string{"https://api.example.com/v1", "http://127.0.0.1:8080/v1"} {
		if isUnsetHost(host) {
			t.Errorf("isUnsetHost(%q) = true, want false", host)
		}
	}
}

func TestShippedProductionAssetIsStructurallyValid(t *testing.T) {
	// The embedded release asset is the one thing no unit test compiles, so
	// assert on it directly. ControlPlaneConfigured is deliberately not
	// asserted: filling in the real host is a release step, not a code change.
	raw, err := os.ReadFile("profiles/production.json")
	if err != nil {
		t.Fatalf("read production.json: %v", err)
	}
	var p Profile
	if err := json.Unmarshal(raw, &p); err != nil {
		t.Fatalf("parse production.json: %v", err)
	}
	if err := p.Validate(); err != nil {
		t.Fatalf("production.json does not satisfy the profile schema: %v", err)
	}
	if !p.IsProduction() {
		t.Fatalf("production.json names profile %q", p.Name)
	}
	if !strings.HasSuffix(p.APIHost, "/v1") || !strings.HasSuffix(p.GatewayHost, "/v1") {
		t.Errorf("control-plane hosts must be versioned (/v1): apiHost=%q gatewayHost=%q", p.APIHost, p.GatewayHost)
	}
}

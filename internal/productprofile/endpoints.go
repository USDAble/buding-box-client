package productprofile

import (
	_ "embed"
	"encoding/json"
	"fmt"
	"strings"
)

// Developer and production builds share these addresses. Edit endpoints.json
// and rebuild to switch deployment; build profiles still own permissions and
// trusted keys. The sealed testing profile owns its address because it either
// starts the fixed loopback stand-in or carries a separately approved test
// deployment address.
//
//go:embed endpoints.json
var embeddedEndpointsJSON string

type endpoints struct {
	APIHost     string `json:"apiHost"`
	GatewayHost string `json:"gatewayHost"`
}

func loadProfile(profileJSON, endpointsJSON string) (Profile, error) {
	var fields map[string]json.RawMessage
	if err := json.Unmarshal([]byte(profileJSON), &fields); err != nil {
		return Profile{}, fmt.Errorf("parse profile: %w", err)
	}
	var p Profile
	if err := json.Unmarshal([]byte(profileJSON), &p); err != nil {
		return Profile{}, fmt.Errorf("parse profile: %w", err)
	}
	if p.Name == Testing {
		if err := p.Validate(); err != nil {
			return Profile{}, fmt.Errorf("validate testing profile: %w", err)
		}
		return p, nil
	}
	for _, key := range []string{"apiHost", "gatewayHost"} {
		if _, exists := fields[key]; exists {
			return Profile{}, fmt.Errorf("profile must not define addresses; edit endpoints.json")
		}
	}
	var addresses endpoints
	if err := json.Unmarshal([]byte(endpointsJSON), &addresses); err != nil {
		return Profile{}, fmt.Errorf("parse endpoints.json: %w", err)
	}
	p.APIHost = strings.TrimRight(strings.TrimSpace(addresses.APIHost), "/")
	p.GatewayHost = strings.TrimRight(strings.TrimSpace(addresses.GatewayHost), "/")
	if err := p.Validate(); err != nil {
		return Profile{}, fmt.Errorf("validate profile with endpoints.json: %w", err)
	}
	return p, nil
}

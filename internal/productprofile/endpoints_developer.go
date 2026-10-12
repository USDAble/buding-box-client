//go:build !product_production && !product_test

package productprofile

import (
	"fmt"
	"net"
	"net/url"
	"strings"
)

// DeveloperAPIHost and DeveloperGatewayHost are linker inputs for a local
// acceptance build. They are absent from production and sealed test binaries;
// user environment/config files cannot change the product's deployment origin.
var DeveloperAPIHost string
var DeveloperGatewayHost string

func loadBuildProfile(profileJSON, endpointsJSON string) (Profile, error) {
	p, err := loadProfile(profileJSON, endpointsJSON)
	if err != nil {
		return p, err
	}
	api, gateway := strings.TrimRight(strings.TrimSpace(DeveloperAPIHost), "/"), strings.TrimRight(strings.TrimSpace(DeveloperGatewayHost), "/")
	if api == "" && gateway == "" {
		return p, nil
	}
	if p.Name != Developer {
		return p, nil
	}
	for name, value := range map[string]string{"apiHost": api, "gatewayHost": gateway} {
		u, err := url.Parse(value)
		if err != nil || u.Scheme != "http" || u.Host == "" || u.User != nil || u.RawQuery != "" || u.Fragment != "" {
			return Profile{}, fmt.Errorf("developer %s override must be an HTTP loopback URL", name)
		}
		ip := net.ParseIP(u.Hostname())
		if u.Hostname() != "localhost" && (ip == nil || !ip.IsLoopback()) {
			return Profile{}, fmt.Errorf("developer %s override must use loopback", name)
		}
	}
	p.APIHost, p.GatewayHost = api, gateway
	p.catalogCacheSource = api
	return p, p.Validate()
}

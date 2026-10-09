//go:build !product_production && !product_test

package productprofile

import "testing"

func TestDeveloperBuildAcceptsOnlyPairedLoopbackLinkerOrigins(t *testing.T) {
	oldAPI, oldGateway := DeveloperAPIHost, DeveloperGatewayHost
	t.Cleanup(func() { DeveloperAPIHost, DeveloperGatewayHost = oldAPI, oldGateway })
	DeveloperAPIHost, DeveloperGatewayHost = "http://127.0.0.1:8000/api/v1", "http://127.0.0.1:8000"
	p, err := loadBuildProfile(embeddedProfileJSON, embeddedEndpointsJSON)
	if err != nil {
		t.Fatal(err)
	}
	if p.APIHost != DeveloperAPIHost || p.GatewayHost != DeveloperGatewayHost {
		t.Fatal("linker origins were not used")
	}
	for _, invalid := range []string{"", "http://example.com", "https://127.0.0.1:8000", "http://user@127.0.0.1:8000", "http://127.0.0.1:8000/?secret=1"} {
		DeveloperGatewayHost = invalid
		if _, err := loadBuildProfile(embeddedProfileJSON, embeddedEndpointsJSON); err == nil {
			t.Fatalf("accepted unsafe/incomplete origin %q", invalid)
		}
	}
	DeveloperAPIHost, DeveloperGatewayHost = "", ""
	p, err = loadBuildProfile(embeddedProfileJSON, embeddedEndpointsJSON)
	if err != nil {
		t.Fatal(err)
	}
	defaults, err := loadProfile(embeddedProfileJSON, embeddedEndpointsJSON)
	if err != nil {
		t.Fatal(err)
	}
	if p.APIHost != defaults.APIHost || p.GatewayHost != defaults.GatewayHost {
		t.Fatal("default deployment changed")
	}
}

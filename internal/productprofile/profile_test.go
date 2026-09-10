package productprofile

import "testing"

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

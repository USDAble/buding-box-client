package brand

import "testing"

func TestLoad(t *testing.T) {
	cfg := Load()
	if cfg.Product.Names["zh-CN"] != "布丁盒子" {
		t.Fatalf("zh-CN product name = %q", cfg.Product.Names["zh-CN"])
	}
	if cfg.Product.Names["en-US"] != "Pudding Box" {
		t.Fatalf("en-US product name = %q", cfg.Product.Names["en-US"])
	}
	if cfg.Compatibility.CLICommand != "octo" {
		t.Fatalf("compatibility CLI command = %q", cfg.Compatibility.CLICommand)
	}
	if cfg.Compatibility.ConfigDir != "~/.octo" {
		t.Fatalf("compatibility config dir = %q", cfg.Compatibility.ConfigDir)
	}
}

func TestNameFallback(t *testing.T) {
	cfg := Load()
	if got := cfg.Name("de-DE"); got != "Pudding Box" {
		t.Fatalf("fallback name = %q", got)
	}
}

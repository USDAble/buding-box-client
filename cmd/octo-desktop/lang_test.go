package main

import (
	"strings"
	"testing"
)

func TestWithBrandUsesConfiguredEnglishDisplayName(t *testing.T) {
	got := withBrand(enStrings, "en-US")
	for _, value := range []string{got.trayShow, got.trayQuit, got.takeoverTitle, got.quitTitle, got.errTitle, got.updTitle} {
		if strings.Contains(value, "Octo") {
			t.Fatalf("user-facing string still contains legacy display name: %q", value)
		}
	}
	if got.takeoverTitle != "Pudding Box" {
		t.Fatalf("takeover title = %q, want Pudding Box", got.takeoverTitle)
	}
}

func TestWithBrandUsesConfiguredChineseDisplayName(t *testing.T) {
	got := withBrand(zhStrings, "zh-CN")
	for _, value := range []string{got.trayShow, got.trayQuit, got.takeoverTitle, got.quitTitle, got.errTitle, got.updTitle} {
		if strings.Contains(value, "Octo") {
			t.Fatalf("user-facing string still contains legacy display name: %q", value)
		}
	}
	if got.takeoverTitle != "布丁盒子" {
		t.Fatalf("takeover title = %q, want 布丁盒子", got.takeoverTitle)
	}
}

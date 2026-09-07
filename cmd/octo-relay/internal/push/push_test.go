package push

import "testing"

// The push payload is content-free by design; the only user-facing text is the
// product name. These assertions lock that text to the configured brand so a
// future edit cannot silently ship the old name in a lock-screen notification.
func TestNotificationTextIsBranded(t *testing.T) {
	if notifTitle != "Pudding Box" {
		t.Errorf("notifTitle = %q, want %q", notifTitle, "Pudding Box")
	}
	if notifBody != "Pudding Box has new activity" {
		t.Errorf("notifBody = %q, want %q", notifBody, "Pudding Box has new activity")
	}
}

func TestLoadProductName(t *testing.T) {
	if got := loadProductName(); got == "" {
		t.Fatal("loadProductName() = empty, want the embedded en-US name")
	}
	if got := loadProductName(); got == "Octo" {
		t.Fatalf("loadProductName() = %q, still the pre-rename name", got)
	}
}

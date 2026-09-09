package productstate

import "testing"

func TestNormalizePhone(t *testing.T) {
	// The three spellings must converge on one canonical form.
	for _, in := range []string{
		"138 0000 1234",
		"+8613800001234",
		"86-138-0000-1234",
		"86 138 0000 1234",
		"138-0000-1234",
	} {
		got, ok := NormalizePhone(in)
		if !ok {
			t.Errorf("NormalizePhone(%q) = not ok", in)
			continue
		}
		if got != "13800001234" {
			t.Errorf("NormalizePhone(%q) = %q, want 13800001234", in, got)
		}
	}
}

func TestNormalizePhoneInvalid(t *testing.T) {
	cases := []struct {
		in  string
		why string
	}{
		{"8613800001", "86 prefix stripped leaves 8 digits, whole string is 10"},
		{"23800001234", "must start with 1"},
		{"1380000123", "10 digits"},
		{"138000012345", "12 digits"},
		{"13a00001234", "contains a letter"},
		{"", "empty"},
		{"+8613800001", "+86 stripped leaves 8 digits"},
	}
	for _, c := range cases {
		if got, ok := NormalizePhone(c.in); ok {
			t.Errorf("NormalizePhone(%q) = %q, want invalid (%s)", c.in, got, c.why)
		}
	}
}

func TestMaskPhone(t *testing.T) {
	if got := MaskPhone("13800001234"); got != "138****1234" {
		t.Errorf("MaskPhone = %q, want 138****1234", got)
	}
	// A malformed input is returned unchanged rather than munged.
	if got := MaskPhone("123"); got != "123" {
		t.Errorf("MaskPhone(malformed) = %q, want unchanged", got)
	}
}

package productphone

import "testing"

func TestNormalize(t *testing.T) {
	for _, tc := range []struct{ in, want string }{
		{"138 0000 1234", "+8613800001234"}, {"+86 138-0000-1234", "+8613800001234"},
		{"+1 (415) 555-0123", "+14155550123"}, {"＋４４ ２０ ７９４６ ０９５８", "+442079460958"},
	} {
		got, ok := Normalize(tc.in)
		if !ok || got != tc.want {
			t.Errorf("Normalize(%q) = %q, %v; want %q, true", tc.in, got, ok, tc.want)
		}
	}
	for _, in := range []string{"11155550123", "+012345678", "+123", "hello"} {
		if got, ok := Normalize(in); ok {
			t.Errorf("Normalize(%q) = %q, true; want false", in, got)
		}
	}
}

func TestNormalizeParts(t *testing.T) {
	for _, tc := range []struct{ phone, regionCode, want string }{
		{"13800001234", "86", "+8613800001234"},
		{"415 555 0123", "1", "+14155550123"},
		{"+442079460958", "", "+442079460958"},
		{"13800001234", "", "+8613800001234"},
	} {
		got, ok := NormalizeParts(tc.phone, tc.regionCode)
		if !ok || got != tc.want {
			t.Errorf("NormalizeParts(%q, %q) = %q, %v; want %q, true", tc.phone, tc.regionCode, got, ok, tc.want)
		}
	}
	for _, tc := range []struct{ phone, regionCode string }{
		{"+8613800001234", "86"}, {"13800001234", "+86"},
		{"13800001234", "0"}, {"13800001234", "1234"},
		{"123", "86"},
	} {
		if got, ok := NormalizeParts(tc.phone, tc.regionCode); ok {
			t.Errorf("NormalizeParts(%q, %q) = %q, true; want false", tc.phone, tc.regionCode, got)
		}
	}
}

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

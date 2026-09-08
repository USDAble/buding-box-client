package productstate

import "testing"

func TestValidateNickname(t *testing.T) {
	// Legal: Han, Latin, digits, underscore, mixed, boundary lengths.
	for _, ok := range []string{
		"用户1234",
		"User",
		"_name_",
		"张三三",
		"aaaaaaaaaaaaaaaa", // exactly 16
		"字a1_",
	} {
		if err := ValidateNickname(ok); err != nil {
			t.Errorf("ValidateNickname(%q) = %v, want nil", ok, err)
		}
	}

	// Illegal lengths and characters.
	for _, bad := range []string{
		"a",                 // 1 rune, below min
		"aaaaaaaaaaaaaaaaa", // 17 runes, above max
		"用户-1",              // hyphen not in set
		"用户 1",              // space not in set
		"🙂🙂",                // emoji not in set
	} {
		if err := ValidateNickname(bad); err == nil {
			t.Errorf("ValidateNickname(%q) = nil, want error", bad)
		}
	}
}

func TestValidateNicknameCountsRunesNotBytes(t *testing.T) {
	// 16 CJK runes is 48 bytes; byte-counting would wrongly reject it.
	if err := ValidateNickname("布丁盒子布丁盒子布丁盒子布丁盒子"); err != nil {
		t.Errorf("16-rune CJK nickname = %v, want nil", err)
	}
}

func TestSensitiveStubNeverMatches(t *testing.T) {
	// The default stub must never flag, or a nickname would be refused before
	// P8 lands the real engine.
	if Sensitive("anything") {
		t.Fatal("stub Sensitive must always return false")
	}
}

func TestSensitiveIsSwappable(t *testing.T) {
	orig := Sensitive
	defer func() { Sensitive = orig }()
	Sensitive = func(v string) bool { return v == "敏感词" }
	if !Sensitive("敏感词") {
		t.Fatal("swapped Sensitive did not run")
	}
	if Sensitive("正常") {
		t.Fatal("swapped Sensitive over-matched")
	}
}

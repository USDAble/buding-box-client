package sensitive

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestBuiltinWordsReturnsCopy(t *testing.T) {
	words := BuiltinWords()
	if len(words) == 0 {
		t.Fatal("BuiltinWords() returned empty")
	}
	// The required baseline (需求 §5.5) must always be present.
	required := map[string]bool{
		"赌博": false, "毒品": false, "发票": false,
		"gambling": false, "drugs": false, "invoice": false, "fapiao": false,
	}
	for _, w := range words {
		if _, ok := required[w]; ok {
			required[w] = true
		}
	}
	for w, found := range required {
		if !found {
			t.Errorf("built-in dictionary is missing required word %q", w)
		}
	}
	// Mutating the returned slice must not affect the package's copy.
	words[0] = "mutated"
	if BuiltinWords()[0] == "mutated" {
		t.Fatal("BuiltinWords() leaked its backing array")
	}
}

func TestUserWordsReadsAndStrips(t *testing.T) {
	path := filepath.Join(t.TempDir(), "sensitive-words.txt")
	os.WriteFile(path, []byte("# header\n\n  苹果  \n发票\n"), 0o600)

	words, err := UserWords(path)
	if err != nil {
		t.Fatalf("UserWords = %v", err)
	}
	if len(words) != 2 || words[0] != "苹果" || words[1] != "发票" {
		t.Fatalf("UserWords = %q, want [苹果 发票]", words)
	}
}

func TestUserWordsMissingFileIsEmpty(t *testing.T) {
	words, err := UserWords(filepath.Join(t.TempDir(), "nope.txt"))
	if err != nil {
		t.Fatalf("UserWords missing = %v, want nil", err)
	}
	if len(words) != 0 {
		t.Fatalf("UserWords missing = %q, want empty", words)
	}
}

func TestUserWordsInvalidUTF8IsError(t *testing.T) {
	path := filepath.Join(t.TempDir(), "sensitive-words.txt")
	os.WriteFile(path, []byte{0xff, 0xfe}, 0o600)
	if _, err := UserWords(path); err == nil {
		t.Fatal("UserWords invalid UTF-8 = nil, want error")
	}
}

func TestNormalizeWord(t *testing.T) {
	cases := []struct {
		in   string
		norm string
		ok   bool
	}{
		{"发票", "发票", true},
		{"发 票", "发票", true},
		{"发-票", "发票", true},
		{"INVOICE", "invoice", true},
		{"!!!", "", false},
		{"   ", "", false},
		{"", "", false},
	}
	for _, c := range cases {
		norm, ok := NormalizeWord(c.in)
		if norm != c.norm || ok != c.ok {
			t.Errorf("NormalizeWord(%q) = (%q, %v), want (%q, %v)", c.in, norm, ok, c.norm, c.ok)
		}
	}
}

func TestMergeUserWords(t *testing.T) {
	// 发 票 normalizes to 发票, so it must be treated as a built-in duplicate.
	merged, added, skipped := MergeUserWords(
		[]string{"测试词"},
		[]string{"苹果", "发 票", "苹果", "!!!", "香蕉"},
	)
	if added != 2 || skipped != 3 {
		t.Fatalf("added/skipped = %d/%d, want 2/3", added, skipped)
	}
	if strings.Join(merged, ",") != "测试词,苹果,香蕉" {
		t.Fatalf("merged = %q, want [测试词 苹果 香蕉]", merged)
	}
}

func TestWriteUserWordsPreservesHeaderComment(t *testing.T) {
	path := filepath.Join(t.TempDir(), "sensitive-words.txt")
	os.WriteFile(path, []byte("# 我的说明\n# 第二行\n\n旧词\n"), 0o600)

	if err := WriteUserWords(path, []string{"新词"}); err != nil {
		t.Fatalf("WriteUserWords = %v", err)
	}
	data, _ := os.ReadFile(path)
	got := string(data)
	if !strings.Contains(got, "# 我的说明\n# 第二行\n") {
		t.Fatalf("header comment block not preserved:\n%s", got)
	}
	if !strings.Contains(got, "新词\n") {
		t.Fatalf("new word not written:\n%s", got)
	}
	if strings.Contains(got, "旧词") {
		t.Fatalf("old word still present after replace:\n%s", got)
	}
}

func TestWriteUserWordsHotReload(t *testing.T) {
	path := filepath.Join(t.TempDir(), "sensitive-words.txt")
	engine := New(path)

	// Write a word through the UI helper, then filter without rebuilding the
	// engine — the ModTime hot reload must pick it up (P7 §3.5 + P13 §7).
	if err := WriteUserWords(path, []string{"香蕉词"}); err != nil {
		t.Fatalf("WriteUserWords = %v", err)
	}
	if got := engine.Filter("香蕉词").Text; got != Mask {
		t.Fatalf("Filter after write = %q, want %q (hot reload failed)", got, Mask)
	}
}

func TestWriteUserWordsAtomicOnMissingDir(t *testing.T) {
	// A path whose parent directory does not exist is created, then written.
	path := filepath.Join(t.TempDir(), "sub", "sensitive-words.txt")
	if err := WriteUserWords(path, []string{"词"}); err != nil {
		t.Fatalf("WriteUserWords = %v", err)
	}
	data, err := os.ReadFile(path)
	if err != nil || string(data) != "词\n" {
		t.Fatalf("read back = %q, %v", data, err)
	}
}

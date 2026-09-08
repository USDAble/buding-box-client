package sensitive

import (
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"
	"unicode/utf8"
)

func TestMissingDictionaryFallsBackToBuiltin(t *testing.T) {
	t.Parallel()

	engine := New(filepath.Join(t.TempDir(), "missing.txt"))
	if got := engine.Filter("赌博").Text; got != Mask {
		t.Fatalf("Text = %q, want %q", got, Mask)
	}
	if !engine.FileUnreadable() {
		t.Fatal("FileUnreadable() = false, want true")
	}
}

func TestEmptyDictionaryKeepsBuiltinWords(t *testing.T) {
	t.Parallel()

	engine := newTestEngine(t, "")
	if got := engine.Filter("毒品").Text; got != Mask {
		t.Fatalf("Text = %q, want %q", got, Mask)
	}
	if engine.FileUnreadable() {
		t.Fatal("FileUnreadable() = true, want false")
	}
}

func TestDictionaryParsingAndNormalization(t *testing.T) {
	t.Parallel()

	engine := newTestEngine(t, "# comment\n\n苹-果\n 苹果 \nINVOICE\n")
	if got := engine.Filter("苹 果").Text; got != Mask {
		t.Fatalf("Text = %q, want %q", got, Mask)
	}
	if got := engine.Filter("invoice").Text; got != Mask {
		t.Fatalf("Text = %q, want %q", got, Mask)
	}
}

func TestSampleDictionary(t *testing.T) {
	t.Parallel()

	engine := New(filepath.Join("testdata", "words.txt"))
	result := engine.Filter("这是测试词")
	if result.Text != "这是***" {
		t.Fatalf("Text = %q, want %q", result.Text, "这是***")
	}
}

func TestBundledDictionary(t *testing.T) {
	t.Parallel()

	engine := New("")
	for _, word := range builtinWords {
		if result := engine.Filter(word); !result.Matched() {
			t.Errorf("bundled word %q was not matched", word)
		}
	}
}

func TestInvalidUTF8DictionaryFallsBackToBuiltin(t *testing.T) {
	t.Parallel()

	path := filepath.Join(t.TempDir(), "sensitive-words.txt")
	if err := os.WriteFile(path, []byte{0xff, 0xfe}, 0o600); err != nil {
		t.Fatalf("write dictionary: %v", err)
	}
	engine := New(path)
	if got := engine.Filter("发票").Text; got != Mask {
		t.Fatalf("Text = %q, want %q", got, Mask)
	}
	if !engine.FileUnreadable() {
		t.Fatal("FileUnreadable() = false, want true")
	}
}

func TestDictionaryHotReload(t *testing.T) {
	t.Parallel()

	path := filepath.Join(t.TempDir(), "sensitive-words.txt")
	writeDictionary(t, path, "苹果\n", time.Now())
	engine := New(path)
	if got := engine.Filter("苹果").Text; got != Mask {
		t.Fatalf("initial Text = %q, want %q", got, Mask)
	}

	writeDictionary(t, path, "香蕉词\n", time.Now().Add(time.Second))
	if got := engine.Filter("苹果").Text; got != "苹果" {
		t.Fatalf("reloaded old word Text = %q, want %q", got, "苹果")
	}
	if got := engine.Filter("香蕉词").Text; got != Mask {
		t.Fatalf("reloaded new word Text = %q, want %q", got, Mask)
	}
}

func TestDictionaryRecoversAfterMissingFileAppears(t *testing.T) {
	t.Parallel()

	path := filepath.Join(t.TempDir(), "sensitive-words.txt")
	engine := New(path)
	if !engine.FileUnreadable() {
		t.Fatal("FileUnreadable() = false before recovery, want true")
	}

	writeDictionary(t, path, "苹果\n", time.Now().Add(time.Second))
	if got := engine.Filter("苹果").Text; got != Mask {
		t.Fatalf("Text after recovery = %q, want %q", got, Mask)
	}
	if engine.FileUnreadable() {
		t.Fatal("FileUnreadable() = true after recovery, want false")
	}
}

func TestEmptyPathUsesBuiltinWithoutFileError(t *testing.T) {
	t.Parallel()

	engine := New("")
	if engine.FileUnreadable() {
		t.Fatal("FileUnreadable() = true, want false")
	}
}

func TestConcurrentFilterAndReload(t *testing.T) {
	path := filepath.Join(t.TempDir(), "sensitive-words.txt")
	writeDictionary(t, path, "苹果\n", time.Now())
	engine := New(path)

	var wg sync.WaitGroup
	for worker := 0; worker < 8; worker++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for i := 0; i < 100; i++ {
				result := engine.Filter("苹果 香蕉 赌博")
				if !utf8.ValidString(result.Text) {
					t.Errorf("invalid UTF-8 output: %q", result.Text)
					return
				}
			}
		}()
	}
	for i := 0; i < 20; i++ {
		word := "苹果\n"
		if i%2 == 1 {
			word = "香蕉\n"
		}
		writeDictionary(t, path, word, time.Now().Add(time.Duration(i+1)*time.Second))
	}
	wg.Wait()
}

func writeDictionary(t testing.TB, path, words string, modified time.Time) {
	t.Helper()
	if err := os.WriteFile(path, []byte(words), 0o600); err != nil {
		t.Fatalf("write dictionary: %v", err)
	}
	if err := os.Chtimes(path, modified, modified); err != nil {
		t.Fatalf("set dictionary timestamp: %v", err)
	}
}

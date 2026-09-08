package sensitive

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"unicode/utf8"
)

func newTestEngine(t testing.TB, words string) *Engine {
	t.Helper()
	path := filepath.Join(t.TempDir(), "sensitive-words.txt")
	if err := os.WriteFile(path, []byte(words), 0o600); err != nil {
		t.Fatalf("write dictionary: %v", err)
	}
	return New(path)
}

func TestFilter(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name  string
		input string
		want  string
	}{
		{name: "substring", input: "增值税发票管理", want: "增值税***管理"},
		{name: "uppercase", input: "INVOICE", want: "***"},
		{name: "mixed case", input: "Invoice", want: "***"},
		{name: "pinyin", input: "fapiao", want: "***"},
		{name: "ASCII separator", input: "发-票", want: "***"},
		{name: "full width space", input: "发　票", want: "***"},
		{name: "adjacent matches", input: "赌博 毒品", want: "***"},
		{name: "separate matches", input: "发票正常毒品", want: "***正常***"},
		{name: "no match", input: "你好", want: "你好"},
		{name: "empty", input: "", want: ""},
		{name: "only stripped symbols", input: "!!!", want: "!!!"},
	}

	engine := New("")
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := engine.Filter(tt.input)
			if got.Text != tt.want {
				t.Fatalf("Filter(%q).Text = %q, want %q", tt.input, got.Text, tt.want)
			}
			if (got.Text != tt.input) != got.Matched() {
				t.Fatalf("Filter(%q).Matched() = %v for output %q", tt.input, got.Matched(), got.Text)
			}
		})
	}
}

func TestFilterMergesOverlappingMatches(t *testing.T) {
	t.Parallel()

	result := newTestEngine(t, "票管\n").Filter("发票管理")
	if result.Text != "***理" {
		t.Fatalf("Text = %q, want %q", result.Text, "***理")
	}
	if len(result.Hits) != 1 {
		t.Fatalf("len(Hits) = %d, want 1", len(result.Hits))
	}
}

func TestFilterReportsOriginalUTF8ByteRange(t *testing.T) {
	t.Parallel()

	input := "🙂发-票🙂"
	result := New("").Filter(input)
	if result.Text != "🙂***🙂" {
		t.Fatalf("Text = %q, want %q", result.Text, "🙂***🙂")
	}
	if len(result.Hits) != 1 {
		t.Fatalf("len(Hits) = %d, want 1", len(result.Hits))
	}
	hit := result.Hits[0]
	wantStart := len("🙂")
	wantEnd := len("🙂发-票")
	if hit.Start != wantStart || hit.End != wantEnd || hit.Word != "发票" {
		t.Fatalf("Hit = %#v, want Start=%d End=%d Word=%q", hit, wantStart, wantEnd, "发票")
	}
}

func TestFilterNoMatchHasNoHits(t *testing.T) {
	t.Parallel()

	input := "原文应逐字节保持不变。"
	result := New("").Filter(input)
	if result.Text != input {
		t.Fatalf("Text = %q, want original %q", result.Text, input)
	}
	if len(result.Hits) != 0 || result.Matched() {
		t.Fatalf("unexpected match: %#v", result)
	}
}

func FuzzFilter(f *testing.F) {
	for _, seed := range []string{"", "你好", "发票", "发-票", "🙂invoice🙂"} {
		f.Add(seed)
	}
	engine := New("")
	f.Fuzz(func(t *testing.T, input string) {
		if !utf8.ValidString(input) {
			t.Skip()
		}
		result := engine.Filter(input)
		if !utf8.ValidString(result.Text) {
			t.Fatalf("Filter returned invalid UTF-8 for %q", input)
		}
		if !result.Matched() && result.Text != input {
			t.Fatalf("no-match output changed: got %q, want %q", result.Text, input)
		}
	})
}

func BenchmarkFilter(b *testing.B) {
	var words strings.Builder
	for i := 0; i < 1000; i++ {
		fmt.Fprintf(&words, "term%04d\n", i)
	}
	engine := newTestEngine(b, words.String())
	input := strings.Repeat("这是一段普通文本normal text 12345。", 300)

	b.ReportAllocs()
	b.SetBytes(int64(len(input)))
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		engine.Filter(input)
	}
}

package sensitive

import (
	"strings"
	"testing"
	"time"
)

func TestStreamFilterAcrossChunks(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name   string
		chunks []string
		want   string
	}{
		{name: "word split", chunks: []string{"发", "票"}, want: Mask},
		{name: "word and separator split", chunks: []string{"发-", "票"}, want: Mask},
		{name: "mixed content", chunks: []string{"hello 发", "票 world"}, want: "hello *** world"},
		{name: "no match", chunks: []string{"hello ", "world"}, want: "hello world"},
	}

	engine := New("")
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			stream := engine.NewStream()
			var output strings.Builder
			for _, chunk := range tt.chunks {
				output.WriteString(stream.Write(chunk))
			}
			output.WriteString(stream.Flush())
			if got := output.String(); got != tt.want {
				t.Fatalf("streamed output = %q, want %q", got, tt.want)
			}
			if got := stream.Flush(); got != "" {
				t.Fatalf("second Flush() = %q, want empty", got)
			}
		})
	}
}

func TestStreamFilterMatchesWholeTextFilter(t *testing.T) {
	t.Parallel()

	input := "开头普通文本 发-票 中间赌博 毒品 末尾"
	engine := New("")
	want := engine.Filter(input).Text
	stream := engine.NewStream()
	chunks := []string{"开头普通", "文本 发-", "票 中间赌", "博 毒", "品 末尾"}

	var output strings.Builder
	for _, chunk := range chunks {
		output.WriteString(stream.Write(chunk))
	}
	output.WriteString(stream.Flush())
	if got := output.String(); got != want {
		t.Fatalf("streamed output = %q, whole-text output = %q", got, want)
	}
}

func TestStreamSnapshotsDictionary(t *testing.T) {
	t.Parallel()

	path := t.TempDir() + "/sensitive-words.txt"
	writeDictionary(t, path, "苹果\n", testTime(1))
	engine := New(path)
	oldStream := engine.NewStream()

	writeDictionary(t, path, "香蕉词\n", testTime(2))
	if got := oldStream.Write("苹果") + oldStream.Flush(); got != Mask {
		t.Fatalf("old stream output = %q, want %q", got, Mask)
	}
	newStream := engine.NewStream()
	if got := newStream.Write("香蕉词") + newStream.Flush(); got != Mask {
		t.Fatalf("new stream output = %q, want %q", got, Mask)
	}
}

func TestStreamKeepsBoundedNormalizedTail(t *testing.T) {
	t.Parallel()

	stream := newTestEngine(t, "abcdefghijklmnop\n").NewStream()
	stream.Write(strings.Repeat("a", 100))
	got := len(normalizeText(stream.pending).spans)
	wantMax := stream.dict.maxLen - 1
	if got > wantMax {
		t.Fatalf("normalized pending length = %d, want <= %d", got, wantMax)
	}
}

func testTime(step int) time.Time {
	return time.Unix(int64(step), 0)
}

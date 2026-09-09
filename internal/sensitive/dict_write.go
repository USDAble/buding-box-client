package sensitive

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// WriteUserWords atomically replaces the user dictionary file, preserving its
// leading comment block. The caller is responsible for validation (empty /
// pure-symbol words, duplicates); this function only writes. Atomic (temp file
// in the same directory, then rename) so a pulled U盘 mid-write cannot leave a
// half-written file behind.
//
// OCTO-FORK: P13 dictionary management — see
// dev-docs-usdable/需求/2260906/技术方案/P13-词库管理界面.md.
func WriteUserWords(path string, words []string) error {
	var header []string
	if data, err := os.ReadFile(path); err == nil {
		header = leadingCommentBlock(string(data))
	}

	var b strings.Builder
	for _, line := range header {
		b.WriteString(line)
		b.WriteByte('\n')
	}
	for _, w := range words {
		b.WriteString(w)
		b.WriteByte('\n')
	}
	return atomicWriteFile(path, []byte(b.String()))
}

// leadingCommentBlock returns the leading run of comment and blank lines, so
// the user's hand-written explanation at the top of the file survives an edit
// made through the UI. It stops at the first word line; comments below words
// are not preserved (an accepted trade-off — see P13 §4.4).
func leadingCommentBlock(content string) []string {
	lines := strings.Split(content, "\n")
	var block []string
	for _, line := range lines {
		trimmed := strings.TrimSpace(line)
		if trimmed == "" || strings.HasPrefix(trimmed, "#") {
			block = append(block, line)
			continue
		}
		break
	}
	return block
}

// atomicWriteFile writes data to a temp file in the target directory and
// renames it over path, so a concurrent reader (the engine's stat-then-read
// hot reload) never observes a torn file.
func atomicWriteFile(path string, data []byte) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return fmt.Errorf("sensitive: create dir for %s: %w", path, err)
	}
	tmp, err := os.CreateTemp(filepath.Dir(path), filepath.Base(path)+".tmp-*")
	if err != nil {
		return fmt.Errorf("sensitive: create temp file for %s: %w", path, err)
	}
	tmpName := tmp.Name()
	defer func() { _ = os.Remove(tmpName) }()

	if _, err := tmp.Write(data); err != nil {
		_ = tmp.Close()
		return fmt.Errorf("sensitive: write temp file: %w", err)
	}
	if err := tmp.Close(); err != nil {
		return fmt.Errorf("sensitive: close temp file: %w", err)
	}
	if err := os.Rename(tmpName, path); err != nil {
		return fmt.Errorf("sensitive: replace %s: %w", path, err)
	}
	return nil
}

//go:build !product_production

package local

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/open-octo/octo-agent/internal/agent"
	"github.com/open-octo/octo-agent/internal/provider"
	"github.com/open-octo/octo-agent/internal/sensitive"
)

// Compile-time: Client satisfies both wire interfaces.
var (
	_ provider.Provider          = (*Client)(nil)
	_ provider.StreamingProvider = (*Client)(nil)
)

func requestWith(user string) provider.Request {
	return provider.Request{
		Model: "buding-local-general",
		Messages: []agent.Message{
			{Role: agent.RoleUser, Content: user},
		},
	}
}

// TestSendGenericReplyHitsSensitiveFilter verifies the generic reply is always
// hit by the output filter (§3.3): Filter(text).Matched() must be true so a
// demo visibly shows "***".
func TestSendGenericReplyHitsSensitiveFilter(t *testing.T) {
	c := &Client{}
	engine := sensitive.New("")

	resp, err := c.Send(context.Background(), requestWith("今天天气怎么样"))
	if err != nil {
		t.Fatalf("Send: %v", err)
	}
	if resp.StopReason != "end_turn" {
		t.Errorf("StopReason = %q, want end_turn", resp.StopReason)
	}
	if !engine.Filter(resp.Content).Matched() {
		t.Errorf("generic reply %q was not hit by the sensitive filter", resp.Content)
	}
	if resp.Content == "" || resp.Blocks[0].Type != "text" {
		t.Errorf("reply missing text block: content=%q blocks=%v", resp.Content, resp.Blocks)
	}
}

// TestSendInvoiceReplyVerifiesOutputFilter confirms the invoice branch embeds
// a sensitive word that the filter masks.
func TestSendInvoiceReplyVerifiesOutputFilter(t *testing.T) {
	c := &Client{}
	engine := sensitive.New("")

	resp, err := c.Send(context.Background(), requestWith("帮我开一张发票"))
	if err != nil {
		t.Fatalf("Send: %v", err)
	}
	if !engine.Filter(resp.Content).Matched() {
		t.Errorf("invoice reply %q was not hit by the sensitive filter", resp.Content)
	}
}

// TestSendEchoesPhone confirms the phone branch echoes the number back so
// P10's PII masking can be eyeballed.
func TestSendEchoesPhone(t *testing.T) {
	c := &Client{}
	resp, err := c.Send(context.Background(), requestWith("我的电话是 13812341234"))
	if err != nil {
		t.Fatalf("Send: %v", err)
	}
	if !strings.Contains(resp.Content, "13812341234") {
		t.Errorf("phone reply %q does not echo the number", resp.Content)
	}
}

// TestChunksSplitSensitiveWordAcrossBoundary pins the stream contract (§3.4):
// the sensitive word 发票 must be delivered across two chunks, never inside a
// single chunk, so P8's StreamFilter is exercised.
func TestChunksSplitSensitiveWordAcrossBoundary(t *testing.T) {
	text := replyText(requestWith("随便聊聊"))
	parts := chunks(text)

	joined := strings.Join(parts, "")
	if joined != text {
		t.Fatalf("chunks do not reassemble: got %q want %q", joined, text)
	}
	if !strings.Contains(joined, "发票") {
		t.Fatalf("reply has no 发票 to split: %q", text)
	}
	for _, p := range parts {
		if strings.Contains(p, "发票") {
			t.Errorf("chunk %q contains the whole sensitive word; it must straddle a boundary", p)
		}
	}
}

// TestStreamEmitsChunksThenAggregates verifies SendStream delivers the same
// text via OnText and returns the aggregated Response.
func TestStreamEmitsChunksThenAggregates(t *testing.T) {
	c := &Client{}
	var streamed []string
	resp, err := c.SendStream(context.Background(), requestWith("你好"), provider.StreamCallbacks{
		OnText: func(d string) { streamed = append(streamed, d) },
	})
	if err != nil {
		t.Fatalf("SendStream: %v", err)
	}
	if got := strings.Join(streamed, ""); got != resp.Content {
		t.Errorf("streamed text = %q, want %q", got, resp.Content)
	}
	if len(streamed) < 2 {
		t.Errorf("expected multiple chunks, got %d", len(streamed))
	}
}

// TestLogWritesJSONL verifies each request appends one valid JSON line with
// role/content messages (§3.5).
func TestLogWritesJSONL(t *testing.T) {
	dir := t.TempDir()
	logPath := filepath.Join(dir, "logs", "local-provider.jsonl")

	c := New(logPath)
	if _, err := c.Send(context.Background(), requestWith("测试请求")); err != nil {
		t.Fatalf("Send: %v", err)
	}

	data, err := os.ReadFile(logPath)
	if err != nil {
		t.Fatalf("read log: %v", err)
	}
	lines := strings.Split(strings.TrimSpace(string(data)), "\n")
	if len(lines) != 1 {
		t.Fatalf("expected 1 log line, got %d: %q", len(lines), string(data))
	}
	var entry logEntry
	if err := json.Unmarshal([]byte(lines[0]), &entry); err != nil {
		t.Fatalf("log line is not valid JSON: %v", err)
	}
	if entry.Model != "buding-local-general" {
		t.Errorf("entry.Model = %q, want buding-local-general", entry.Model)
	}
	if len(entry.Messages) != 1 || entry.Messages[0].Role != "user" || entry.Messages[0].Content != "测试请求" {
		t.Errorf("entry.Messages = %+v, want one user message", entry.Messages)
	}
}

// TestLogEmptyPathSkipsWriting ensures a Client with no log path degrades
// gracefully (tests / callers without a data root).
func TestLogEmptyPathSkipsWriting(t *testing.T) {
	c := &Client{} // logPath == ""
	if _, err := c.Send(context.Background(), requestWith("hi")); err != nil {
		t.Fatalf("Send with no log path: %v", err)
	}
}

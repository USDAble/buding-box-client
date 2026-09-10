//go:build !product_production

// Package local implements the P11 demo/offline fake model channel.
//
// It is not a real backend: Send/SendStream return fixed, deterministic
// replies that always contain a built-in sensitive word (so output filtering
// visibly fires in demos), stream in rune-sized chunks with a deliberate
// boundary inside a sensitive word (so P8's cross-chunk StreamFilter has a
// real, reproducible test path), and log every request to
// data/logs/local-provider.jsonl (the P10 PII observability aid).
//
// OCTO-FORK: P11 假模型通道 — 纯新增 provider 子包，走 .octorules「新 provider」
// 扩展路径，零上游冲突。见
// dev-docs-usdable/需求/2260906/技术方案/P11-假模型通道.md.
//
// This whole package is compiled out of production builds (`product_production`).
// Deleting ensureLocalEndpoint() removed the automatic *seeding* of config.yml
// with buding-* endpoints, but it did not remove the reply engine itself: it
// was still linked into the shipped binary, so "production cannot use it"
// rested on a runtime selection check. The build tag makes it structural. The
// production build gets local_production.go instead — same exported names, no
// demo text, no PII log writer, hard failure on use.
package local

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"time"

	"github.com/open-octo/octo-agent/internal/agent"
	"github.com/open-octo/octo-agent/internal/provider"
)

// Name is the stable provider identifier ("local"), used in logs/telemetry.
const Name = "local"

// streamInterval paces one chunk per tick, simulating a real streaming
// backend. Tests construct a Client with a zero interval to skip the wait.
const streamInterval = 30 * time.Millisecond

// Client is the demo fake model channel. It implements provider.Provider and
// provider.StreamingProvider (the two interfaces provider.go actually
// defines — the design doc's "ToolStreamingProvider" predates the current
// interface set). The app adapter lifts it to the full agent sender stack, so
// the agent exercises the identical streaming path a real provider would.
type Client struct {
	// logPath is where each request is appended (data/logs/local-provider.jsonl).
	// Empty disables logging — tests and callers without a data root.
	logPath string

	// interval paces streamed chunks. Zero means "no wait" (tests).
	interval time.Duration
}

// New returns a Client that logs requests to logPath and paces its stream at
// the default cadence. A logPath of "" disables request logging.
func New(logPath string) *Client {
	return &Client{logPath: logPath, interval: streamInterval}
}

// Name implements provider.Provider.
func (c *Client) Name() string { return Name }

// Send implements provider.Provider. It produces the fixed reply for req and
// returns it as a single completed turn.
func (c *Client) Send(ctx context.Context, req provider.Request) (provider.Response, error) {
	c.log(req)
	text := replyText(req)
	return response(req, text), nil
}

// SendStream implements provider.StreamingProvider. It delivers the fixed
// reply rune-chunk by rune-chunk through cb.OnText, then returns the
// aggregated Response — the same contract the wire providers honour.
func (c *Client) SendStream(ctx context.Context, req provider.Request, cb provider.StreamCallbacks) (provider.Response, error) {
	c.log(req)
	text := replyText(req)
	for _, chunk := range chunks(text) {
		if err := ctx.Err(); err != nil {
			return provider.Response{}, err
		}
		if cb.OnText != nil {
			cb.OnText(chunk)
		}
		if c.interval > 0 {
			select {
			case <-ctx.Done():
				return provider.Response{}, ctx.Err()
			case <-time.After(c.interval):
			}
		}
	}
	return response(req, text), nil
}

// response builds the completed-turn Response for the fixed reply.
func response(req provider.Request, text string) provider.Response {
	return provider.Response{
		Content:      text,
		Blocks:       []agent.ContentBlock{agent.NewTextBlock(text)},
		Model:        req.Model,
		StopReason:   "end_turn",
		OutputTokens: len([]rune(text)),
	}
}

// logEntry is one line of data/logs/local-provider.jsonl. It records exactly
// what the model was asked — the P10 PII observability aid ("实际请求里是
// 138****1234"). messages only carries role/content: the fake model never
// sees tools, so blocks are irrelevant here.
type logEntry struct {
	At       string       `json:"at"`
	Model    string       `json:"model"`
	Messages []logMessage `json:"messages"`
}

type logMessage struct {
	Role    string `json:"role"`
	Content string `json:"content,omitempty"`
}

// log appends one line per request. It is best-effort and thread-safe enough
// for the demo: each call opens/append/closes, so concurrent turns interleave
// only at line granularity (a torn line is still valid JSON on its own line).
func (c *Client) log(req provider.Request) {
	if c.logPath == "" {
		return
	}
	entry := logEntry{
		At:       time.Now().Format(time.RFC3339),
		Model:    req.Model,
		Messages: make([]logMessage, 0, len(req.Messages)),
	}
	for _, m := range req.Messages {
		entry.Messages = append(entry.Messages, logMessage{
			Role:    string(m.Role),
			Content: messageText(m),
		})
	}
	data, err := json.Marshal(entry)
	if err != nil {
		return
	}
	if err := os.MkdirAll(filepath.Dir(c.logPath), 0o700); err != nil {
		return
	}
	f, err := os.OpenFile(c.logPath, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o600)
	if err != nil {
		return
	}
	defer f.Close()
	_, _ = f.Write(append(data, '\n'))
}

// messageText returns the plain-text view of one message: Content when set,
// else the joined text blocks (a user turn may carry tool_result blocks but
// those carry no prose worth logging for a demo).
func messageText(m agent.Message) string {
	if m.Content != "" {
		return m.Content
	}
	var out string
	for _, b := range m.Blocks {
		if b.Type == "text" {
			out += b.Text
		}
	}
	return out
}

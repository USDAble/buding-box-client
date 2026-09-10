//go:build product_production

// OCTO-FORK: P11 — production counterpart of the local fake channel.
//
// The deterministic reply engine, its canned demo text (which deliberately
// contains sensitive words) and the data/logs/local-provider.jsonl PII writer
// live in files tagged !product_production and are therefore absent from the
// shipped binary. What remains here is the minimum that keeps
// internal/app/sender.go compiling unchanged: the same exported names, with
// every call failing hard.
//
// Why a stub instead of deleting the package: sender.go is an upstream file
// with an existing OCTO-FORK hunk that dispatches `provider: local`. Keeping
// the exported surface stable means the build tag change touches only this
// package, and any production config or catalog that mentions the local
// provider still resolves to a named provider with a precise error rather
// than "unknown provider".
//
// This is a defence in depth, not the primary control: P0-05 must also refuse
// to select a local model for a production session. The difference is that
// this file makes the failure *structural* — a production binary physically
// cannot emit a canned reply — whereas a selection check is a runtime
// condition that a later refactor could drop.
package local

import (
	"context"
	"fmt"

	"github.com/open-octo/octo-agent/internal/provider"
)

// Name is the stable provider identifier ("local"). Kept so config parsing and
// diagnostics still name the provider instead of reporting it as unknown.
const Name = "local"

// Client exists only so internal/app/sender.go compiles unchanged. It holds no
// state: there is no reply engine to configure and no log file to write.
type Client struct{}

// New mirrors the developer signature (logPath is ignored) so the caller needs
// no build tags of its own.
func New(logPath string) *Client { return &Client{} }

// Name implements provider.Provider.
func (c *Client) Name() string { return Name }

// Send implements provider.Provider. It always fails: the local channel is a
// developer/test capability and is not present in production builds.
func (c *Client) Send(ctx context.Context, req provider.Request) (provider.Response, error) {
	return provider.Response{}, errUnavailable()
}

// SendStream implements provider.StreamingProvider. It always fails without
// emitting a single token, so no caller can observe partial fake output.
func (c *Client) SendStream(ctx context.Context, req provider.Request, cb provider.StreamCallbacks) (provider.Response, error) {
	return provider.Response{}, errUnavailable()
}

func errUnavailable() error {
	return fmt.Errorf("provider %q is not available in production builds", Name)
}

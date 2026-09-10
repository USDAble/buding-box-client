// Package runtimeport holds the neutral extension seams a host process may use
// to supply behaviour the upstream server would otherwise decide itself.
//
// Nothing here may name a product concern: the point of a port is that the
// server learns "the host decides this", never why. See
// dev-docs-usdable/需求/20260909/P0-正式服务与Agent演进/01A-既有产品逻辑迁出internal-server.md
// §3 for the ports this fork is allowed to add and §3.1 for this one.
package runtimeport

import (
	"context"
	"errors"

	"github.com/open-octo/octo-agent/internal/agent"
)

// ErrNoSender is returned when a factory reports success but hands back a nil
// sender, which would otherwise surface as a nil dereference on the first turn
// rather than as a configuration error.
var ErrNoSender = errors.New("runtimeport: sender factory returned no sender")

// SenderFactory supplies the agent.Sender one turn runs on. A host that installs
// one has taken responsibility for the vendor, the endpoint and the credentials.
type SenderFactory interface {
	// SenderForTurn returns the sender and the model id for one turn.
	//
	// Returning an error fails the turn. The server does not fall back to
	// resolving a sender from its own configuration — that fallback is exactly
	// the bypass this port exists to close, so it is deliberately not the
	// caller's to choose.
	SenderForTurn(ctx context.Context, req SenderRequest) (agent.Sender, string, error)
}

// SenderRequest is what the server tells the factory about the turn it is about
// to run. Every field is read-only for the factory.
type SenderRequest struct {
	// Session is the conversation the turn belongs to. May be nil when the
	// server is resolving a sender outside any session (startup, or a global
	// settings change).
	Session *agent.Session
	// Model is the model id the session asked for, if any.
	Model string
}

// Resolve asks a host factory for one turn's sender.
//
// ok is false when no factory is installed, which means the caller must resolve
// the sender its own way. That is the only case in which a caller may fall back:
// with a factory installed, a non-nil err is returned as-is, because falling
// back to a config-resolved sender is the bypass this port exists to close.
//
// A factory that reports success but returns a nil sender yields ErrNoSender
// rather than a nil that would panic on the first turn.
//
// The policy lives here, not in the server, so that the server only ever learns
// "the host decides this" — never the precedence rules or the reason for them.
func Resolve(f SenderFactory, req SenderRequest) (sender agent.Sender, model string, ok bool, err error) {
	if f == nil {
		return nil, "", false, nil
	}
	sender, model, err = f.SenderForTurn(context.Background(), req)
	if err != nil {
		return nil, "", true, err
	}
	if sender == nil {
		return nil, "", true, ErrNoSender
	}
	return sender, model, true, nil
}

// FailingSender is an agent.Sender whose every call returns err.
//
// It exists because a turn's sender is resolved in some places that cannot
// return an error — senderForSession, for instance, already has a session in
// hand. Handing back a sender that always fails keeps the failure visible,
// which is the whole point: the alternative, returning the default sender,
// is the silent degradation this port was added to remove.
func FailingSender(err error) agent.Sender { return failingSender{err: err} }

type failingSender struct{ err error }

func (f failingSender) SendMessages(context.Context, string, string, []agent.Message, int) (agent.Reply, error) {
	return agent.Reply{}, f.err
}

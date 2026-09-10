//go:build !product_production

// Package mock provides the contract sample for the gateway control plane.
// Like productclient/mock it is excluded from production builds: a cancel that
// reports success without contacting the platform is exactly the "fabricated
// authority" the release build must not be able to link.
package mock

import (
	"context"
	"sync"

	"github.com/open-octo/octo-agent/internal/agent"
	"github.com/open-octo/octo-agent/internal/productclient"
	"github.com/open-octo/octo-agent/internal/productclient/gateway"
)

// Control is a scripted gateway.ControlClient that records what it was asked to
// do, so a test can assert "cancel was called exactly once with this id" instead
// of inferring it from side effects.
type Control struct {
	mu        sync.Mutex
	cancelled []string
	statuses  map[string]productclient.RequestStatus

	CancelFunc        func(ctx context.Context, clientRequestID string) error
	RequestStatusFunc func(ctx context.Context, clientRequestID string) (productclient.RequestStatus, error)
}

var _ gateway.ControlClient = (*Control)(nil)

// NewControl returns a Control whose Cancel always succeeds and whose
// RequestStatus serves whatever was seeded with SetStatus.
func NewControl() *Control {
	return &Control{statuses: map[string]productclient.RequestStatus{}}
}

// SetStatus seeds the terminal state for one clientRequestId.
func (c *Control) SetStatus(clientRequestID string, status productclient.RequestStatus) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.statuses == nil {
		c.statuses = map[string]productclient.RequestStatus{}
	}
	c.statuses[clientRequestID] = status
}

// Cancelled returns the ids Cancel was called with, in order.
func (c *Control) Cancelled() []string {
	c.mu.Lock()
	defer c.mu.Unlock()
	return append([]string(nil), c.cancelled...)
}

func (c *Control) Cancel(ctx context.Context, clientRequestID string) error {
	c.mu.Lock()
	c.cancelled = append(c.cancelled, clientRequestID)
	fn := c.CancelFunc
	c.mu.Unlock()
	if fn != nil {
		return fn(ctx, clientRequestID)
	}
	return nil
}

func (c *Control) RequestStatus(ctx context.Context, clientRequestID string) (productclient.RequestStatus, error) {
	if c.RequestStatusFunc != nil {
		return c.RequestStatusFunc(ctx, clientRequestID)
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	if s, ok := c.statuses[clientRequestID]; ok {
		return s, nil
	}
	// An id the platform never saw is not "settled". Returning a zero
	// state would read as falsy-but-successful; say so instead.
	return productclient.RequestStatus{}, &productclient.Error{
		Op: "RequestStatus", Code: productclient.CodeModelNotFound, HTTPStatus: 404,
		Message: "unknown clientRequestId",
	}
}

// Recorder is a gateway.Observer that records the metadata it was given and
// returns its base sender untouched.
//
// Returning base unchanged is the one implementation that trivially satisfies
// the capability-preservation rule, which makes it useful for wiring tests; it
// is not a substitute for the real observer because it never reports terminal
// state.
type Recorder struct {
	mu    sync.Mutex
	metas []gateway.CallMeta
	bases []agent.Sender
	sink  gateway.TerminalSink
}

var _ gateway.Observer = (*Recorder)(nil)

// Metas returns the call metadata seen so far, in order.
func (r *Recorder) Metas() []gateway.CallMeta {
	r.mu.Lock()
	defer r.mu.Unlock()
	return append([]gateway.CallMeta(nil), r.metas...)
}

// Wrap records the call and returns base unchanged.
func (r *Recorder) Wrap(base agent.Sender, meta gateway.CallMeta, sink gateway.TerminalSink) agent.Sender {
	r.mu.Lock()
	r.metas = append(r.metas, meta)
	r.bases = append(r.bases, base)
	r.sink = sink
	r.mu.Unlock()
	return base
}

// Sink returns the terminal sink handed to the most recent Wrap.
func (r *Recorder) Sink() gateway.TerminalSink {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.sink
}

// Sink is a gateway.TerminalSink that records what the observer reported, so a
// test can assert on the sequence instead of on a side effect.
type Sink struct {
	mu        sync.Mutex
	metas     []gateway.CallMeta
	terminals []productclient.RequestStatus
}

var _ gateway.TerminalSink = (*Sink)(nil)

func (s *Sink) OnMeta(meta gateway.CallMeta) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.metas = append(s.metas, meta)
}

func (s *Sink) OnTerminal(status productclient.RequestStatus) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.terminals = append(s.terminals, status)
}

// Metas returns the metadata notifications, in order.
func (s *Sink) Metas() []gateway.CallMeta {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]gateway.CallMeta(nil), s.metas...)
}

// Terminals returns the terminal notifications, in order.
func (s *Sink) Terminals() []productclient.RequestStatus {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]productclient.RequestStatus(nil), s.terminals...)
}

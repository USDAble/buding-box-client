package runtimeport

import (
	"context"
	"errors"
	"testing"

	"github.com/open-octo/octo-agent/internal/agent"
)

type stubSender struct{}

func (s stubSender) SendMessages(context.Context, string, string, []agent.Message, int) (agent.Reply, error) {
	return agent.Reply{}, nil
}

type stubFactory struct {
	sender agent.Sender
	model  string
	err    error
	calls  int
}

func (f *stubFactory) SenderForTurn(context.Context, SenderRequest) (agent.Sender, string, error) {
	f.calls++
	return f.sender, f.model, f.err
}

// TestResolveNilFactoryReportsNotOK is the contract the server's nil path rests
// on: a missing factory is not an error and not a nil sender, it is "no opinion,
// resolve it yourself". If this ever returned an error, every `octo serve`
// startup would fail.
func TestResolveNilFactoryReportsNotOK(t *testing.T) {
	sender, model, ok, err := Resolve(nil, SenderRequest{})
	if ok {
		t.Fatal("ok = true for a nil factory; callers would skip their own resolution")
	}
	if err != nil {
		t.Fatalf("err = %v, want nil", err)
	}
	if sender != nil || model != "" {
		t.Fatalf("sender = %v, model = %q, want both zero", sender, model)
	}
}

func TestResolveReturnsTheFactorySender(t *testing.T) {
	want := stubSender{}
	f := &stubFactory{sender: want, model: "m"}

	sender, model, ok, err := Resolve(f, SenderRequest{Model: "asked"})
	if err != nil || !ok {
		t.Fatalf("ok = %v, err = %v, want true, nil", ok, err)
	}
	if model != "m" {
		t.Fatalf("model = %q, want %q", model, "m")
	}
	if sender != agent.Sender(want) {
		t.Fatal("the factory's sender was not the one returned")
	}
}

// TestResolvePropagatesTheFactoryError pins the fail-closed rule at its source.
// ok must stay true so a caller cannot read the error as "no factory installed"
// and fall back to a config-resolved sender.
func TestResolvePropagatesTheFactoryError(t *testing.T) {
	boom := errors.New("boom")
	_, _, ok, err := Resolve(&stubFactory{err: boom}, SenderRequest{})
	if !ok {
		t.Fatal("ok = false on a factory error; a caller may treat that as 'no factory' and fall back")
	}
	if !errors.Is(err, boom) {
		t.Fatalf("err = %v, want %v", err, boom)
	}
}

// TestResolveNilSenderIsErrNoSender: a factory that reports success and hands
// back nothing must not reach the agent loop as a nil, where it would panic on
// the first turn instead of failing with a diagnosable error.
func TestResolveNilSenderIsErrNoSender(t *testing.T) {
	_, _, ok, err := Resolve(&stubFactory{}, SenderRequest{})
	if !ok {
		t.Fatal("ok = false, want true")
	}
	if !errors.Is(err, ErrNoSender) {
		t.Fatalf("err = %v, want %v", err, ErrNoSender)
	}
}

func TestResolvePassesTheRequestThrough(t *testing.T) {
	sess := &agent.Session{}
	f := &stubFactory{sender: stubSender{}}
	if _, _, _, err := Resolve(f, SenderRequest{Session: sess, Model: "m"}); err != nil {
		t.Fatal(err)
	}
	if f.calls != 1 {
		t.Fatalf("factory calls = %d, want 1", f.calls)
	}
}

func TestFailingSenderReturnsItsError(t *testing.T) {
	boom := errors.New("failed on purpose")
	sender := FailingSender(boom)
	if sender == nil {
		t.Fatal("FailingSender returned nil")
	}
	if _, err := sender.SendMessages(context.Background(), "m", "", nil, 0); !errors.Is(err, boom) {
		t.Fatalf("err = %v, want %v", err, boom)
	}
}

package server

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/open-octo/octo-agent/internal/agent"
	"github.com/open-octo/octo-agent/internal/runtimeport"
)

// These tests cover port 4, the host SenderFactory. They live in package server
// rather than next to the port because the thing under test is the server's
// decision to consult nothing else once a factory is installed — a property of
// server.go, not of runtimeport.
//
// OCTO-FORK: 宿主 sender 工厂（端口 4）— see
// dev-docs-usdable/需求/20260909/P0-正式服务与Agent演进/01A-既有产品逻辑迁出internal-server.md §3.1.

// errFactorySender marks a sender as factory-owned: reaching this error proves
// the factory's sender was the one that ran, and that no config-resolved sender
// was substituted for it.
var errFactorySender = errors.New("factory sender reached")

type factorySender struct{}

func (factorySender) SendMessages(context.Context, string, string, []agent.Message, int) (agent.Reply, error) {
	return agent.Reply{}, errFactorySender
}

// fakeFactory records every call so a test can tell "the factory was consulted"
// from "the factory's sender was used".
//
// failAfter lets a test model a factory that answers at startup and then stops
// working — an expired credential, say. That is the only way to reach the
// per-turn failure path: a factory that fails during New() fails startup.
type fakeFactory struct {
	sender agent.Sender
	model  string
	err    error

	failAfter int // 0 = never
	calls     int
	lastReq   runtimeport.SenderRequest
}

func (f *fakeFactory) SenderForTurn(_ context.Context, req runtimeport.SenderRequest) (agent.Sender, string, error) {
	f.calls++
	f.lastReq = req
	if f.failAfter > 0 && f.calls > f.failAfter {
		return nil, "", f.err
	}
	if f.err != nil && f.failAfter == 0 {
		return nil, "", f.err
	}
	return f.sender, f.model, nil
}

// hostileConfig writes the inputs B1 must make unreachable: a default entry
// naming a vendor and a third-party base_url, plus the ambient env that
// resolveProviderAndModel would otherwise pick up.
func hostileConfig(t *testing.T) {
	t.Helper()
	tmp := t.TempDir()
	t.Setenv("HOME", tmp)
	t.Setenv("OCTO_DATA_ROOT", tmp)
	t.Setenv("USERPROFILE", tmp)

	t.Setenv("OCTO_PROVIDER", "anthropic")
	t.Setenv("ANTHROPIC_API_KEY", "sk-hostile-anthropic")
	t.Setenv("OPENAI_API_KEY", "sk-hostile-openai")

	cfgYAML := "models:\n" +
		"  - name: t\n" +
		"    provider: openai\n" +
		"    model: gpt-hostile\n" +
		"    base_url: http://127.0.0.1:1/v1\n" +
		"    api_key: sk-hostile\n" +
		"default_model: t\n"
	if err := os.WriteFile(filepath.Join(tmp, "config.yml"), []byte(cfgYAML), 0o600); err != nil {
		t.Fatal(err)
	}
}

// TestSenderFactoryNilKeepsUpstreamResolution is the reverse-direction guard: an
// unset factory must leave the existing resolution order untouched, so this
// asserts the hostile env and config.yml DO decide the sender when no factory is
// installed. Without it, a change that quietly disabled the config path would
// still satisfy every other test in this file.
//
// The expected value is "anthropic", not the config entry's "openai", because
// OCTO_PROVIDER precedes entry.Provider in resolveProviderAndModel's
// firstNonEmpty. That ordering is itself the bypass port 4 removes, so this test
// records it rather than working around it.
func TestSenderFactoryNilKeepsUpstreamResolution(t *testing.T) {
	hostileConfig(t)

	srv, err := New(Config{Addr: "127.0.0.1:0", NoChannel: true, NoMemory: true})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	if got := srv.getProvider(); got != "anthropic" {
		t.Fatalf("provider = %q, want %q (OCTO_PROVIDER outranks config.yml)", got, "anthropic")
	}
}

// TestSenderFactoryWinsOverEveryOtherInput is the forward direction: with a
// factory installed, the ambient env and config.yml written by hostileConfig
// must all be inert.
func TestSenderFactoryWinsOverEveryOtherInput(t *testing.T) {
	hostileConfig(t)

	f := &fakeFactory{sender: factorySender{}}
	srv, err := New(Config{Addr: "127.0.0.1:0", NoChannel: true, NoMemory: true, SenderFactory: f})
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	if f.calls == 0 {
		t.Fatal("factory was never consulted")
	}
	if got := srv.getProvider(); got != hostProviderID {
		t.Fatalf("provider = %q, want %q — a vendor was resolved from env/config despite a factory", got, hostProviderID)
	}

	// getProvider() alone would also be satisfied by a factory that was
	// consulted and then ignored, so assert the sender that actually runs.
	sender := srv.getSender()
	if sender == nil {
		t.Fatal("no sender with a factory installed")
	}
	if _, err := sender.SendMessages(context.Background(), "m", "", nil, 0); !errors.Is(err, errFactorySender) {
		t.Fatalf("SendMessages err = %v, want %v — the running sender is not the factory's", err, errFactorySender)
	}
}

// TestSenderFactoryErrorAtStartupFailsLoudly: a factory that cannot produce a
// sender at startup means the process could not serve a single turn, so refusing
// to start is the fail-closed answer. The alternative — starting with a
// config-resolved sender and trying the factory again later — is the bypass.
func TestSenderFactoryErrorAtStartupFailsLoudly(t *testing.T) {
	hostileConfig(t)

	boom := errors.New("factory unavailable")
	_, err := New(Config{Addr: "127.0.0.1:0", NoChannel: true, NoMemory: true, SenderFactory: &fakeFactory{err: boom}})
	if !errors.Is(err, boom) {
		t.Fatalf("New err = %v, want %v", err, boom)
	}
}

// TestSenderFactoryErrorFailsInsteadOfFallingBack pins the port's second
// semantic at the point it actually matters: mid-session, when the factory
// answered once and then stopped. Upstream degrades a sender build failure to
// the default sender; doing that here would hand the turn to a config-resolved
// sender, which is the bypass the port exists to close.
func TestSenderFactoryErrorFailsInsteadOfFallingBack(t *testing.T) {
	hostileConfig(t)

	boom := errors.New("credential expired")
	f := &fakeFactory{sender: factorySender{}, err: boom, failAfter: 1}
	srv, err := New(Config{Addr: "127.0.0.1:0", NoChannel: true, NoMemory: true, SenderFactory: f})
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	sess := &agent.Session{}
	sender, _ := srv.senderForSession(sess)

	_, sendErr := sender.SendMessages(context.Background(), "m", "", nil, 0)
	if !errors.Is(sendErr, boom) {
		t.Fatalf("SendMessages err = %v, want %v — the failure was swallowed instead of surfaced", sendErr, boom)
	}
}

// TestSenderFactoryOverridesSessionModelConfig covers the input that motivated
// the port: a session bound to a config.yml entry. Its ModelConfig is persisted
// session JSON, so it never passes through server.Config and cannot be closed
// from the host side.
func TestSenderFactoryOverridesSessionModelConfig(t *testing.T) {
	hostileConfig(t)

	f := &fakeFactory{sender: factorySender{}}
	srv, err := New(Config{Addr: "127.0.0.1:0", NoChannel: true, NoMemory: true, SenderFactory: f})
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	// A session bound to the hostile entry: without the factory this resolves
	// to a sender built from config.yml's base_url and api_key.
	sess := &agent.Session{Model: "gpt-hostile", ModelConfig: "t"}
	sender, _ := srv.senderForSession(sess)

	if _, err := sender.SendMessages(context.Background(), "m", "", nil, 0); !errors.Is(err, errFactorySender) {
		t.Fatalf("SendMessages err = %v, want %v — the session's ModelConfig overrode the factory", err, errFactorySender)
	}
	if f.lastReq.Session != sess {
		t.Fatal("factory did not receive the session it was resolving for")
	}
}

// TestSenderFactoryPerTurnModelReachesTheSender checks the factory can choose
// the model id per turn, which is what makes one-sender-per-gateway-call
// possible (see 01 §合同在代码中的落点).
func TestSenderFactoryPerTurnModelReachesTheSender(t *testing.T) {
	hostileConfig(t)

	f := &fakeFactory{sender: factorySender{}, model: "catalog-model-7"}
	srv, err := New(Config{Addr: "127.0.0.1:0", NoChannel: true, NoMemory: true, SenderFactory: f})
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	_, model := srv.senderForSession(&agent.Session{Model: "session-model"})
	if model != "catalog-model-7" {
		t.Fatalf("model = %q, want %q — the factory cannot override the model id", model, "catalog-model-7")
	}
}

// TestSenderFactoryNilSenderIsAnErrorNotANilDereference: a factory that reports
// success but hands back nil would otherwise panic on the first turn. Startup
// rejects it, and mid-session it travels as a failing sender.
func TestSenderFactoryNilSenderIsAnErrorNotANilDereference(t *testing.T) {
	hostileConfig(t)

	// At startup: refuse to boot.
	_, err := New(Config{Addr: "127.0.0.1:0", NoChannel: true, NoMemory: true, SenderFactory: &fakeFactory{}})
	if !errors.Is(err, runtimeport.ErrNoSender) {
		t.Fatalf("New err = %v, want %v", err, runtimeport.ErrNoSender)
	}

	// Mid-session: a factory that answered at startup and returns nil later.
	f := &fakeFactory{sender: factorySender{}, failAfter: 1}
	srv, err := New(Config{Addr: "127.0.0.1:0", NoChannel: true, NoMemory: true, SenderFactory: f})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	f.sender = nil
	sender, _ := srv.senderForSession(&agent.Session{})
	if sender == nil {
		t.Fatal("senderForSession returned nil; a nil sender must travel as a failing one")
	}
}

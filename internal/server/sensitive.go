// OCTO-FORK: the product's compliance-word engine and its one assembly point —
// see dev-docs-usdable/需求/20260911/开发计划.md §PR-6a.
package server

import (
	"github.com/open-octo/octo-agent/internal/agent"
	"github.com/open-octo/octo-agent/internal/app"
	"github.com/open-octo/octo-agent/internal/datapath"
	"github.com/open-octo/octo-agent/internal/sensitive"
)

// NewSensitiveEngine builds the one engine a process uses.
//
// A data root that cannot be resolved degrades to the built-in words instead of
// failing startup (B4: an unconfigured product must still start), and a missing
// dictionary file is a legal state — the embedded list serves and nothing is
// created (开发规范 §3.9.1). The dictionary's NAME lives in internal/sensitive
// (sensitive.DictFileName), because the package that reads the file is the one
// that must name it: the dictionary routes in internal/productruntime write
// that same file and may not import this package (see PR-6b2 in
// dev-docs-usdable/需求/20260911/开发计划.md).
//
// WHY IT IS EXPORTED AND WIRED FROM OUTSIDE. The engine is needed on two sides
// that must not import each other: this package masks model output on the turn
// path, and internal/productruntime answers the input-check and dictionary
// routes (PR-6b). Building one per side would give the same file two readers
// with two caches, so "the effective word list" could differ between the screen
// and the checker (开发规范 §3.8). cmd/octo-desktop's mountProductAPI therefore
// calls this once and hands the result to both (人工拍板 2026-09-14).
//
// New still falls back to this when Config.SensitiveEngine is nil, so the CLI
// (`octo serve`) keeps masking without knowing the product exists.
func NewSensitiveEngine() *sensitive.Engine {
	p, err := datapath.Join(sensitive.DictFileName)
	if err != nil {
		return sensitive.New("")
	}
	return sensitive.New(p)
}

// sensitiveEngineOr is the injection seam: the process's engine when the build
// supplied one, its own otherwise. Nil is the CLI's shape, never a way to turn
// masking off — a nil engine does pass through app.WrapSensitive unchanged, so
// treating nil as "no filtering" would be a silent compliance hole.
func sensitiveEngineOr(injected *sensitive.Engine) *sensitive.Engine {
	if injected != nil {
		return injected
	}
	return NewSensitiveEngine()
}

// wrapSensitive decorates the sender a turn's agent is built on, so every
// visible piece of model text is masked before the agent stores it — the
// session file therefore holds the masked text (G2: "落盘即打码"), which is
// what makes a restart re-read the same `***`.
//
// WHY HERE AND NOT AT THE SENDER'S SOURCE. The turns this build serves are not
// served by the sender the server assembled at startup: a gateway-bound turn
// gets a sender built per turn (GatewayEndpoint.Sender), and an IM turn gets
// one from the channel model ops. Wrapping only the startup sender — which is
// what the archived implementation did, before the gateway existed — would
// leave the product's own turn path unfiltered while every test stayed green.
// The three call sites are the three places a turn's agent is constructed
// (buildAgent, the sub-agent template, the IM channel), so no path reaches a
// user or the session file without the filter.
//
// nil-safe: a nil engine or nil sender passes through unchanged
// (app.WrapSensitive), so a test that builds a server without a data root keeps
// working.
func (s *Server) wrapSensitive(sender agent.Sender) agent.Sender {
	return app.WrapSensitive(sender, s.sensitiveEngine)
}

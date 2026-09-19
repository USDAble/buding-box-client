// OCTO-FORK: the product's compliance-word engine and its one assembly point —
// see the current implementation plan §PR-6a.
package server

import (
	"net/http"

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
// the current implementation plan).
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

// codeInputSensitive is the code a refused input carries (本地API契约 §3, and the
// WS event of §4). It names the message the user just typed, not a fault of the
// session, so it is deliberately NOT in this package's fieldLevelCodes table:
// that table maps a code onto a form field to redden, and the refusal already
// carries the replacement text for the input box.
//
// It is a client-local code, like codeModelWithdrawn in turn_refusal.go: the
// value crosses into JavaScript and the two ends are two literals
// (ChatView.svelte:912 on the other side) because no build step can compare Go
// to TS. Its registry is 中台交付包 §3.2.
const codeInputSensitive = "input_sensitive"

// sensInputVerdict consults the injected input gate (需求 D1).
//
// The nil check is the only thing this package decides: no gate means a build
// with no product assembly (`octo serve`) or a test that is not about the gate,
// and in both cases invoking a nil func would panic while "no gate" must mean
// "nothing is refused". The judgement itself — the user's switch plus the one
// engine — stays in internal/productruntime, which owns both facts
// (开发规范 §3.8); this package never learns where they came from.
//
// Callers must consult this BEFORE they broadcast or persist the user's message:
// the order is the requirement (see 开发计划 §PR-6b3), because a refusal after
// the fact leaves a question on screen that was never asked.
func (s *Server) sensInputVerdict(text string) (string, bool) {
	if s.cfg.SensitiveInputGate == nil {
		return "", false
	}
	return s.cfg.SensitiveInputGate(text)
}

// refuseSensitiveInput writes the REST refusal for an input the gate rejected.
//
// A 400 with the code and the masked text, rather than a turn_error: no turn
// exists yet, and the two facts the caller needs are "why" and "what to put in
// the box instead" (the archived shape, kept because the contract registers it).
func refuseSensitiveInput(w http.ResponseWriter, masked string) {
	writeJSON(w, http.StatusBadRequest, map[string]any{
		"code": codeInputSensitive,
		"text": masked,
	})
}

// broadcastInputSensitive tells the window that its message was refused, so the
// composer can put the masked text back and show the notice. The shape is the
// contract's ({session_id, text}) because ChatView.svelte:912 already consumes
// it: it ignores an event for another session and substitutes `text` verbatim.
//
// The event is broadcast rather than sent to one connection: a session can be
// open in two windows, and both are showing the same input box.
func (s *Server) broadcastInputSensitive(sid, masked string) {
	if s.wsHub == nil {
		return
	}
	s.wsHub.broadcast(sid, map[string]any{
		"type":       "input_sensitive",
		"session_id": sid,
		"text":       masked,
	})
}

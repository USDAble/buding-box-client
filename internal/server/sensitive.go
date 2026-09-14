// OCTO-FORK: the product's compliance-word engine and its one assembly point —
// see dev-docs-usdable/需求/20260911/开发计划.md §PR-6a.
package server

import (
	"github.com/open-octo/octo-agent/internal/agent"
	"github.com/open-octo/octo-agent/internal/app"
	"github.com/open-octo/octo-agent/internal/datapath"
	"github.com/open-octo/octo-agent/internal/sensitive"
)

// sensitiveDictName is the user-extensible word list inside the data root.
// Missing is a legal state, not a fault: the engine then serves its embedded
// list, and nothing is created — absence means "use the default", never
// "materialise the default" (开发规范 §3.9.1). A portable data root copied from
// another machine may legitimately carry the user's own list, which is why the
// path is resolved through datapath rather than compiled in (hard rule 1).
const sensitiveDictName = "sensitive-words.txt"

// newSensitiveEngine builds the one engine this server instance uses. A data
// root that cannot be resolved degrades to the built-in words instead of
// failing startup (B4: an unconfigured product must still start).
func newSensitiveEngine() *sensitive.Engine {
	p, err := datapath.Join(sensitiveDictName)
	if err != nil {
		return sensitive.New("")
	}
	return sensitive.New(p)
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

package productruntime

import (
	"fmt"
	"strings"

	"github.com/open-octo/octo-agent/internal/agent"
	"github.com/open-octo/octo-agent/internal/app"
	"github.com/open-octo/octo-agent/internal/productclient"
)

// GatewayEndpoint is the built-in gateway endpoint (需求基线 C1) expressed as a
// sender factory, and it is the only genuinely new piece of PR-5a.
//
// WHY A FACTORY RATHER THAN A SENDER. C2 规则 1 keeps the access token in memory
// and never on disk, and 规则 2 says the sender is assembled from whatever token
// is current. So a sender cannot be built once and cached: it would freeze the
// token it was built with. This type is therefore a value that is cheap to hold
// and whose Sender method is called per turn — the caller in internal/server
// treats it as "how to reach the gateway", not as "the gateway connection".
//
// WHY THE TWO HALVES ARE SEPARATE FIELDS. Host is a build fact (it comes from
// the profile) while Tokens is a run-time fact (it belongs to the signed-in
// user). Keeping them apart is what lets a test dial a stand-in gateway with a
// real holder, which the embedded profile cannot otherwise be made to do.
//
// WHY IT IS NOT A method ON Runtime. Runtime owns the product's local API and
// its state, and none of that is needed here; a function of two inputs with no
// shared state is testable on its own and cannot accidentally read the catalog
// or the disk. The assembly in cmd/octo-desktop is what pairs it with the same
// CredentialHolder the platform client uses, so a refresh is visible here
// without any extra plumbing (V-32 established that the holder is the one place
// a restored session lands).
type GatewayEndpoint struct {
	// Host is the gateway's base URL: scheme and authority, optionally with a
	// /v1 segment.
	//
	// It is written bare in the profile, but the provider accepts either shape:
	// internal/provider/openai's endpointURL trims a trailing /v1 before
	// appending "/v1/chat/completions", so both dial the same path. This comment
	// used to state the opposite as a hard requirement (V-37, retracted — the
	// normalisation is twenty lines below the constant that was misread, and
	// upstream's own client_test.go pins it).
	//
	// The genuinely asymmetric half is the control plane: internal/productclient
	// concatenates caller-supplied paths, so its host does have to carry /v1.
	Host string
	// Tokens is the in-memory credential holder. nil means the caller never
	// wired one — a wiring mistake, refused rather than panicked on.
	Tokens *productclient.CredentialHolder
}

// Sender builds the sender for one gateway turn.
//
// It reads the token at call time, so a refresh that happened between turns is
// picked up with no invalidation step. It deliberately performs no refresh of
// its own: C2 规则 3 requires construction to stay free of I/O, because the
// single-flight refresh belongs to the client that owns the credential, and a
// token that expired mid-turn must be handled (and the request replayed once)
// there rather than here.
//
// The errors are what a signed-out or mis-wired build produces instead of a
// request, and each names which half is missing. They are English like every
// other server-side turn error on this path, and they matter beyond
// tidiness: C9 forbids falling back to any other model source, so a turn that
// cannot be served must fail here rather than be handed to the default sender.
//
// WHY IT TAKES A TUNING (PR-5b1). The two reasoning preferences are handed in
// rather than looked up here, for the same reason the token is: they change at
// run time, and this endpoint cannot see the caller's config. It does not read
// them itself because internal/config's values are read in exactly one place on
// behalf of senders — internal/server — and a fork package holding a config
// handle would be a second reader (开发规范 §3.8). The caller resolves them per
// turn for the same reason it calls this method per turn.
func (g GatewayEndpoint) Sender(tuning app.ReasoningTuning) (agent.Sender, error) {
	if strings.TrimSpace(g.Host) == "" {
		return nil, fmt.Errorf("the built-in gateway has no address in this build; the turn was not started and nothing was sent")
	}
	if g.Tokens == nil {
		return nil, fmt.Errorf("no credential holder is wired for the built-in gateway; the turn was not started and nothing was sent")
	}
	token := g.Tokens.AccessToken()
	if token == "" {
		return nil, fmt.Errorf("not signed in: the built-in gateway needs a session token and none is held; the turn was not started and nothing was sent")
	}

	// C1 规则 1: provider = custom, protocol = openai. Both are required — a
	// Custom endpoint has no registry-pinned wire format, so the protocol is
	// what selects the client. Everything after this is upstream: the SSE
	// framing, the tool_calls index reassembly, the usage normalisation and the
	// Accept/Authorization headers all come from internal/provider/openai, which
	// is exactly why C3 forbids writing a second parser.
	//
	// ReasoningEffort and ShowReasoning are forwarded verbatim, both halves. The
	// tuner is the user's, and the two do different jobs: the effort asks the
	// model to reason at all (and is omitted on the wire when empty — the "off"
	// level), while ShowReasoning decides whether a trace that comes back reaches
	// the event stream. Wiring only one of them is a half-fix that looks whole:
	// show-only never asks a model to think, effort-only receives the trace and
	// drops it (app.sender.reasoningSink).
	//
	// Dialect is deliberately left unset. It selects which of five reasoning
	// field shapes internal/provider/openai emits, so the empty value means "the
	// default branch" — a flat reasoning_effort — and that is the shape the
	// contract now names (中台交付包 §5.2, PQ27); the alternative shapes are what
	// Bailian, DeepSeek's native API, OpenRouter and Kimi each need. Setting it
	// here would be guessing a contract fact, and if the real gateway disagrees
	// this field is the one place that changes.
	return app.NewSender(app.SenderOptions{
		Provider:        app.ProviderCustom,
		Protocol:        "openai",
		APIKey:          token,
		BaseURL:         g.Host,
		ReasoningEffort: tuning.ReasoningEffort,
		ShowReasoning:   tuning.ShowReasoning,
	})
}

package productruntime

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/open-octo/octo-agent/internal/agent"
	"github.com/open-octo/octo-agent/internal/app"
	"github.com/open-octo/octo-agent/internal/productclient"
)

// gatewayEnsureBudget bounds the token exchange Sender may run when the holder
// has no access token (V-39).
//
// A budget rather than a bare call because the turn is waiting on it: an
// endpoint that accepts the connection and never answers would otherwise hold
// the turn open with nothing on screen to explain it, and "we are waiting for a
// token" is indistinguishable from "we are working" (开发规范 §3.9). Ten seconds
// is several times a healthy exchange while staying short enough that the user
// gets an error rather than silence.
const gatewayEnsureBudget = 10 * time.Second

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
	// Ensure obtains an access token when the holder has none, and nil means the
	// old behaviour: no token, no turn.
	//
	// WHY IT IS HERE AND NOT SOMEWHERE ELSE (PR-4c1). A relaunched process holds
	// nothing but a refresh token, and this factory is the first place that
	// discovers it: the turn cannot be built without a bearer, and until V-39
	// there was nobody to ask for one — the only authorised call any production
	// code made was the login-time catalog fetch, which does not happen on a
	// restart. Putting the ask here costs nothing upstream (internal/server does
	// not change by a byte, and it is the caller that decides when a sender is
	// wanted), and it puts the exchange exactly where the need is rather than at
	// startup, where it would have to write the credential file before the user
	// did anything (§3.9.1).
	//
	// The alternative — threading a context down from the turn path — was
	// rejected: senderForSession and buildAgent have no context today, so it
	// would touch four upstream call sites and five test files to improve the
	// cancellation story only. The budget below is what takes its place.
	//
	// WHAT IT IS NOT. It is not a second refresh path: it is handed
	// productruntime.SessionRenewer.Ensure, which calls the client's own
	// single-flight exchange and then persists the rotation (V-42). This type
	// neither knows nor decides how a token is obtained.
	Ensure func(context.Context) error
}

// Sender builds the sender for one gateway turn.
//
// It reads the token at call time, so a refresh that happened between turns is
// picked up with no invalidation step.
//
// It performs no exchange of its own EXCEPT when the holder has no access token
// at all (V-39): then, and only then, it asks Ensure for one under the budget
// above. The two halves of C2 规则 3 are kept apart deliberately — an expired
// token mid-turn is still the client's business, and the single-flight refresh
// still belongs to the code that owns the credential; what changed is that "no
// token has ever been obtained in this process" became a state someone answers
// for. That is the one scope in which construction touches the network, and it
// is a state the user cannot fix any other way.
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
	if g.Tokens.AccessToken() == "" && g.Ensure != nil {
		ctx, cancel := context.WithTimeout(context.Background(), gatewayEnsureBudget)
		defer cancel()
		if err := g.Ensure(ctx); err != nil {
			// The cause is carried into the message rather than swallowed: "the
			// turn did not start" is the same sentence for a refused session, a
			// dead control plane and a timeout, and only one of the three is
			// fixed by signing in again.
			return nil, fmt.Errorf("not signed in: the built-in gateway needs a session token and obtaining one failed (%w); the turn was not started and nothing was sent", err)
		}
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

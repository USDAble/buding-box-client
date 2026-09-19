// Command productstub runs the Central Platform stand-in as a real process, so
// the desktop app can be pointed at it and the product flows walked by hand.
//
// It is the same server the tests use (internal/productclient/clienttest) —
// nothing is reimplemented for manual runs, and every fixture behaves exactly as
// the automated end-to-end tests assert. It serves the platform boundary; it is
// not a substitute for the local Go service, which is a different layer.
//
// SCOPE (开发规范 §3.10): developer, local, run-time only. It is never built into
// a shipped artifact — packaging targets build cmd/octo-desktop, and this binary
// is not referred to by any of them. It listens on loopback and holds no
// credentials, because its whole purpose is to be a stand-in for a platform that
// is not reachable yet (需求基线 A1 规则 7 / S-8): the Sandbox address is not
// available, so the developer profile points here instead.
//
// The alternative — pointing developer.json at a real Sandbox — is the eventual
// state, and changing the profile is the only step needed when the address
// arrives. Until then this keeps the activation path walkable.
//
// Usage:
//
//	go run ./cmd/productstub                     # 127.0.0.1:8788
//	go run ./cmd/productstub :9123               # a different port
//	go run ./cmd/productstub -tool=terminal      # make the gateway ask for a tool
//
//	go run ./cmd/productstub \
//	  -upstream=https://api.deepseek.com/v1 -model=deepseek-chat
//	# ^ model turns go to a REAL provider instead of the canned reply.
//	#   Pass the key with -key or OCTO_UPSTREAM_KEY; the platform token is
//	#   replaced on the way out and never forwarded.
//
// Then set internal/productprofile/profiles/developer.json's apiHost to
// http://127.0.0.1:<port>/v1 and its gatewayHost to http://127.0.0.1:<port> and
// launch the desktop build.
//
// The two hosts take different shapes, but only the apiHost's is a requirement:
// the control-plane client concatenates its own paths onto apiHost, so /v1 must
// be there, while the gateway client appends and normalises the path itself, so
// either gatewayHost shape works. This banner used to print /v1 for both and
// called the difference mandatory (V-37, retracted).
package main

import (
	"flag"
	"fmt"
	"log"
	"net"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/open-octo/octo-agent/internal/productclient/clienttest"
)

const defaultAddr = "127.0.0.1:8788"

// upstreamKeyEnv is where -key falls back to. A flag one can see in the shell
// history is better than one typed on the command line, so the env var is the
// documented form and the flag is for one-off runs.
const upstreamKeyEnv = "OCTO_UPSTREAM_KEY"

// toolArguments maps -tool values to what the stand-in will ask the client to
// run. Only tools whose arguments can be written down in advance appear here:
// the fixture is a stand-in, not a model, and inventing a path at run time would
// make the walkthrough unrepeatable.
//
// terminal is the interesting one. Its command is `hostname` because that
// matches no rule in internal/permission/defaults.yml and therefore falls
// through to the engine's implicit ask - so the local permission prompt really
// does appear. `echo` would not: the policy auto-allows the common safe verbs,
// and the walkthrough would quietly demonstrate the allow path instead (which is
// what the nails' first draft did, and why the premise is asserted there).
// read_file needs no prompt at all - the policy allows any read - so it shows
// the other half: a call that runs with no user involvement.
var toolArguments = map[string]string{
	"terminal":  `{"command":"hostname"}`,
	"read_file": `{"path":"go.mod"}`,
}

func main() {
	toolName := flag.String("tool", "", "make the stand-in ask for this tool (terminal | read_file); empty means the healthy shape, no tool call")
	upstream := flag.String("upstream", "", "forward model turns to a real OpenAI-compatible provider at this base URL instead of answering from the fixture, e.g. https://api.deepseek.com/v1")
	upstreamKey := flag.String("key", "", "the provider's API key (defaults to $"+upstreamKeyEnv+"); required with -upstream and never forwarded to the control plane")
	upstreamModel := flag.String("model", "", "send this model id upstream instead of the catalog id; a real provider rejects the fixture ids, so this is required in practice")
	var inj inject
	registerInject(flag.CommandLine, &inj)
	flag.Parse()
	addr := defaultAddr
	if rest := flag.Args(); len(rest) > 1 {
		// Go's flag package stops at the first positional argument, so
		// `productstub :8802 -tool=x` lands here with the switch unparsed.
		// Refusing is right - silently dropping it would make the walkthrough
		// look like "the gateway declined to ask for a tool" - and the message
		// says which way round the arguments go instead of just complaining.
		log.Fatalf("productstub: at most one positional argument (the address), and flags come before it; got %v. Try: productstub -tool=%s %s",
			rest, *toolName, rest[0])
	} else if len(rest) == 1 {
		addr = rest[0]
	}
	if *toolName != "" {
		if _, ok := toolArguments[*toolName]; !ok {
			log.Fatalf("productstub: -tool=%s has no pre-scripted arguments; known: %v", *toolName, knownTools())
		}
	}
	if err := validateFlags(*toolName, *upstream); err != nil {
		log.Fatalf("productstub: %v", err)
	}

	key := *upstreamKey
	if key == "" {
		key = os.Getenv(upstreamKeyEnv)
	}
	gateway := gatewayFlags{baseURL: *upstream, apiKey: key, model: *upstreamModel}
	if gateway.enabled() && gateway.apiKey == "" {
		// Refused rather than dialled and 401'd: the provider's own answer for a
		// missing key is unhelpful about where the key should come from.
		log.Fatalf("productstub: -upstream needs a provider key; pass -key=... or set $%s", upstreamKeyEnv)
	}

	// Loopback only. Binding wider would put a fake platform — one that hands out
	// tokens for a fixed SMS code — on the network.
	ln, err := net.Listen("tcp", addr)
	if err != nil {
		log.Fatalf("productstub: listen on %s: %v", addr, err)
	}

	stub := clienttest.New()
	if *toolName != "" {
		stub.RequestToolCall(*toolName, toolArguments[*toolName])
	}
	inj.apply(stub)

	printFixtures(ln.Addr().String(), *toolName, gateway, inj)

	if err := http.Serve(ln, withRequestLog(withUpstreamGateway(stub.Handler(), stub, gateway))); err != nil {
		log.Fatalf("productstub: serve: %v", err)
	}
}

func knownTools() []string {
	out := make([]string, 0, len(toolArguments))
	for name := range toolArguments {
		out = append(out, name)
	}
	return out
}

// withRequestLog prints one line per request the stand-in answers.
//
// WHY IT LIVES HERE AND NOT IN clienttest. The library is also used by in-process
// tests, where a line per request is noise, and the question this answers only
// exists for a hand-run process: "is the desktop build really talking to this
// stand-in, or is something else answering?" Until this existed the stand-in was
// silent, so "the flow works" and "the stand-in was reached" were
// indistinguishable — which is exactly how the frontend's own stand-in for the
// local /api boundary (web/src/dev, removed by PR-3) could answer every call
// while looking like a working backend (需求基线 V-9). A silent substitute is
// indistinguishable from no substitute.
//
// The completions line is marked because that is the one that proves a model turn
// crossed the platform boundary rather than reaching a third-party endpoint —
// 需求基线 B4's whole point, and what V-35 violated. The log prints after the
// response, so its duration is the turn's.
func withRequestLog(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Default 200: a streaming handler that never calls WriteHeader is
		// answering 200 implicitly, and reporting a misleading 0 would be worse
		// than reporting nothing.
		rec := &statusRecorder{ResponseWriter: w, status: http.StatusOK}
		start := time.Now()
		next.ServeHTTP(rec, r)
		mark := ""
		if strings.HasSuffix(r.URL.Path, "/chat/completions") {
			mark = "   <-- a model turn through the platform boundary"
		}
		log.Printf("%s %s -> %d (%s)%s",
			r.Method, r.URL.Path, rec.status, time.Since(start).Round(time.Millisecond), mark)
	})
}

// statusRecorder remembers the status so the log line can report it.
type statusRecorder struct {
	http.ResponseWriter
	status int
}

func (r *statusRecorder) WriteHeader(code int) {
	r.status = code
	r.ResponseWriter.WriteHeader(code)
}

// printFixtures states what the stand-in accepts. Without this the walkthrough
// turns into guesswork: these values are fixed by the fixture constants, and
// they are the same ones the fake frontend backend used, so they may already
// look familiar.
//
// The tool section prints on every run, including when the switch is off. A
// walkthrough that does not say whether the gateway will ask for a tool is a
// walkthrough that cannot tell "the tool loop is broken" from "the switch is
// off" - and an unexplained approval prompt is worse than a missing one.
func printFixtures(addr, toolName string, gateway gatewayFlags, inj inject) {
	base := "http://" + addr
	toolLine := "  tool calls   OFF - the gateway will not ask for any tool (the healthy shape)"
	if toolName != "" {
		toolLine = fmt.Sprintf("  tool calls   ON - the gateway will ask for %s with %s\n"+
			"                 once; it stops asking as soon as the result comes back",
			toolName, toolArguments[toolName])
	}
	fmt.Printf(`productstub: Central Platform stand-in listening on %s

  apiHost      %s/v1
  gatewayHost  %s        (either shape works; this one matches the client default)

%s

%s

  Accepted inputs (fixed by internal/productclient/clienttest):
    phone              any 11 digits starting with 1, e.g. 13800001234
    SMS code           %s
    activation code    %s   (pairs with %s)
                       %s   (same box, unused — the one-to-many rule)
                       %s   (issued to %s only)
    box code           %s
    nickname           1-20 characters

%s

  Endpoints: POST %s/v1/auth/sms/send, /auth/login, /auth/refresh
             GET  %s/v1/client/bootstrap
             GET  %s/v1/catalog/models
	     GET  %s/v1/box
	     POST %s/v1/feedback             (structured, user-authored feedback)
             POST %s/v1/chat/completions     (the built-in gateway: streams,
                                              needs a Bearer access token)

  State is in memory: restarting this process resets every consumed code,
    which is what makes the single-use activation path repeatable. It also
    re-issues the same refresh tokens, so clear the client's data/credential.json
    when you restart this process to test from a fresh install (V-51).
`,
		addr, base, base,
		describeGateway(gateway),
		inj.describe(),
		clienttest.FixtureSMSCode,
		clienttest.FixtureActivationCode, clienttest.FixtureBoxCode,
		clienttest.FixtureSecondActivationCode,
		clienttest.FixtureBoundPhoneActivationCode, clienttest.FixtureBoundPhone,
		clienttest.FixtureBoxCode,
		toolLine,
		base, base, base, base, base, base)
}

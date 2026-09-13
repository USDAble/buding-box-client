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
//	go run ./cmd/productstub            # 127.0.0.1:8788
//	go run ./cmd/productstub :9123      # a different port
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

func main() {
	addr := defaultAddr
	if len(os.Args) > 1 {
		addr = os.Args[1]
	}

	// Loopback only. Binding wider would put a fake platform — one that hands out
	// tokens for a fixed SMS code — on the network.
	ln, err := net.Listen("tcp", addr)
	if err != nil {
		log.Fatalf("productstub: listen on %s: %v", addr, err)
	}

	printFixtures(ln.Addr().String())

	if err := http.Serve(ln, withRequestLog(clienttest.New().Handler())); err != nil {
		log.Fatalf("productstub: serve: %v", err)
	}
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
func printFixtures(addr string) {
	base := "http://" + addr
	fmt.Printf(`productstub: Central Platform stand-in listening on %s

  apiHost      %s/v1
  gatewayHost  %s        (either shape works; this one matches the client default)

  Accepted inputs (fixed by internal/productclient/clienttest):
    phone              any 11 digits starting with 1, e.g. 13800001234
    SMS code           %s
    activation code    %s   (pairs with %s)
                       %s   (same box, unused — the one-to-many rule)
                       %s   (issued to %s only)
    box code           %s
    nickname           1-20 characters

  Endpoints: POST %s/v1/auth/sms/send, /auth/login, /auth/refresh
             GET  %s/v1/client/bootstrap
             POST %s/v1/chat/completions     (the built-in gateway: streams,
                                              needs a Bearer access token)

  State is in memory: restarting this process resets every consumed code,
  which is what makes the single-use activation path repeatable.
`,
		addr, base, base,
		clienttest.FixtureSMSCode,
		clienttest.FixtureActivationCode, clienttest.FixtureBoxCode,
		clienttest.FixtureSecondActivationCode,
		clienttest.FixtureBoundPhoneActivationCode, clienttest.FixtureBoundPhone,
		clienttest.FixtureBoxCode,
		base, base, base)
}

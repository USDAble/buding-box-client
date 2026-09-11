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
// Then set internal/productprofile/profiles/developer.json's apiHost and
// gatewayHost to http://127.0.0.1:<port>/v1 and launch the desktop build.
package main

import (
	"fmt"
	"log"
	"net"
	"net/http"
	"os"

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

	if err := http.Serve(ln, clienttest.New().Handler()); err != nil {
		log.Fatalf("productstub: serve: %v", err)
	}
}

// printFixtures states what the stand-in accepts. Without this the walkthrough
// turns into guesswork: these values are fixed by the fixture constants, and
// they are the same ones the fake frontend backend used, so they may already
// look familiar.
func printFixtures(addr string) {
	base := "http://" + addr + "/v1"
	fmt.Printf(`productstub: Central Platform stand-in listening on %s

  apiHost / gatewayHost  http://%s/v1

  Accepted inputs (fixed by internal/productclient/clienttest):
    phone              any 11 digits starting with 1, e.g. 13800001234
    SMS code           %s
    activation code    %s   (pairs with %s)
                       %s   (same box, unused — the one-to-many rule)
                       %s   (issued to %s only)
    box code           %s
    nickname           1-20 characters

  Endpoints: POST %s/auth/sms/send, /auth/login, /auth/refresh
             GET  %s/client/bootstrap

  State is in memory: restarting this process resets every consumed code,
  which is what makes the single-use activation path repeatable.
`,
		addr, addr,
		clienttest.FixtureSMSCode,
		clienttest.FixtureActivationCode, clienttest.FixtureBoxCode,
		clienttest.FixtureSecondActivationCode,
		clienttest.FixtureBoundPhoneActivationCode, clienttest.FixtureBoundPhone,
		clienttest.FixtureBoxCode,
		base, base)
}

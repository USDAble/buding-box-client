package server

import (
	"os"
	"testing"

	"github.com/open-octo/octo-agent/internal/agent"
	"github.com/open-octo/octo-agent/internal/app"
	"github.com/open-octo/octo-agent/internal/config"
)

// PR-5b1's seam half: the reasoning preferences have to reach the gateway
// factory, and they have to be read per turn rather than captured when the
// server was assembled.
//
// WHAT THIS FIXES. The gateway factory was handed the host and the token and
// nothing else, so a gateway turn never asked the model to reason and never
// surfaced a trace it did receive. The user-visible result was the opposite of
// every other endpoint in the app: internal/config's own endpoints show thinking
// by default (EffectiveShowReasoning answers true when nothing is configured),
// while the one endpoint a product build actually uses could not show it at all.
//
// WHY THE NAILS ASSERT WHAT THE FACTORY RECEIVED rather than the request body:
// the body is asserted one layer down, in internal/productruntime and against a
// real stand-in (gateway_reasoning_test.go). Here the question is only whether
// the two preferences arrive, and from where.
//
// WHY "FROM WHERE" IS WORTH A NAIL. The values are run-time mutable: PATCH
// /api/config/show_reasoning and PATCH /api/sessions/{id}/reasoning_effort both
// change them between turns, while the GatewayEndpoint is built once at
// assembly. A seam that captured them would pin the first turn's answer for the
// life of the process, which is the same class of bug as caching the gateway
// sender (C2 规则 2) — and it would be invisible without this nail.

// writeReasoningConfig writes the two preferences to the file the server itself
// reads them from, at the path config resolves.
//
// It goes through config.Path() rather than joining a path by hand: 开发规范 §3.1
// makes internal/datapath the only place that resolves a data path, and a test
// that guessed one would keep passing after the real path moved.
func writeReasoningConfig(t *testing.T, effort string, show bool) {
	t.Helper()
	path, err := config.Path()
	if err != nil {
		t.Fatalf("config.Path: %v", err)
	}
	body := "reasoning_effort: " + effort + "\n"
	if show {
		body += "show_reasoning: true\n"
	} else {
		body += "show_reasoning: false\n"
	}
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatalf("write %s: %v", path, err)
	}
}

// gatewayTuningServer is a product-shaped server whose gateway factory records
// the tuning it was handed.
//
// It is built on emptyProfileServer so it inherits the same "no provider at all"
// state the other gateway nails use (B4 makes that the product's normal state),
// and it records every call rather than the last one, because one nail below is
// about the difference between two calls.
func gatewayTuningServer(t *testing.T) (*Server, *[]app.ReasoningTuning) {
	t.Helper()
	var seen []app.ReasoningTuning
	srv := emptyProfileServer(t, Config{
		Addr:               "127.0.0.1:0",
		GatewayModelPrefix: testGatewayPrefix,
		GatewaySender: func(tuning app.ReasoningTuning) (agent.Sender, error) {
			seen = append(seen, tuning)
			return &countingSender{}, nil
		},
	})
	return srv, &seen
}

func TestTheFactoryIsHandedTheReasoningPreferences(t *testing.T) {
	srv, seen := gatewayTuningServer(t)
	writeReasoningConfig(t, "medium", true)

	srv.senderForSession(boundSession("buding-gateway::buding-privacy-1", ""))

	if len(*seen) != 1 {
		t.Fatalf("the factory was asked %d time(s), want 1", len(*seen))
	}
	got := (*seen)[0]
	if got.ReasoningEffort != "medium" {
		t.Errorf("ReasoningEffort = %q, want \"medium\" — the level the user picked never reached the gateway", got.ReasoningEffort)
	}
	if !got.ShowReasoning {
		t.Error("ShowReasoning = false, want true — the trace would be dropped on the way back")
	}
}

// The per-turn half. Two turns with a preference changed in between must see two
// different answers: the first turn's values must not be captured.
func TestThePreferencesAreReadAgainForEachTurn(t *testing.T) {
	srv, seen := gatewayTuningServer(t)
	writeReasoningConfig(t, "medium", true)

	sess := boundSession("buding-gateway::buding-privacy-1", "")
	srv.senderForSession(sess)

	writeReasoningConfig(t, "high", false)
	srv.senderForSession(sess)

	if len(*seen) != 2 {
		t.Fatalf("the factory was asked %d time(s) across two turns, want 2", len(*seen))
	}
	if first := (*seen)[0]; first.ReasoningEffort != "medium" || !first.ShowReasoning {
		t.Errorf("first turn got %+v, want the values configured then", first)
	}
	second := (*seen)[1]
	if second.ReasoningEffort != "high" {
		t.Errorf("second turn got ReasoningEffort %q, want \"high\" — the tuning was captured at assembly rather than read per turn", second.ReasoningEffort)
	}
	if second.ShowReasoning {
		t.Error("second turn got ShowReasoning = true, want false — the switch was captured at assembly")
	}
}

// The system default, asserted because it is the fact that made this a defect
// rather than a missing feature: with nothing configured, reasoning is SHOWN.
// A build that wired the preference but defaulted it to false would look like it
// had fixed nothing, and a nail that configured show_reasoning: true would not
// notice.
func TestTheSystemDefaultShowsReasoning(t *testing.T) {
	srv, seen := gatewayTuningServer(t) // no config.yml written at all

	srv.senderForSession(boundSession("buding-gateway::buding-privacy-1", ""))

	if len(*seen) != 1 {
		t.Fatalf("the factory was asked %d time(s), want 1", len(*seen))
	}
	got := (*seen)[0]
	if !got.ShowReasoning {
		t.Error("ShowReasoning = false with nothing configured, want the system default (true) — this is the value config.EffectiveShowReasoning already answers for every other endpoint")
	}
	if got.ReasoningEffort != "" {
		t.Errorf("ReasoningEffort = %q with nothing configured, want \"\" (off)", got.ReasoningEffort)
	}
}

package main

import (
	"flag"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/open-octo/octo-agent/internal/productclient/clienttest"
)

// parseInject runs the switches through a private FlagSet, so the test never
// touches flag.CommandLine — a test that parsed the process's own flags would
// leak its switches into every later test in the package.
func parseInject(t *testing.T, args ...string) (inject, error) {
	t.Helper()
	var in inject
	fs := flag.NewFlagSet("test", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	registerInject(fs, &in)
	if err := fs.Parse(args); err != nil {
		return in, err
	}
	return in, nil
}

// TestTheUnsetSwitchesLeaveTheHealthyShape is the premise every other test in
// this file rests on: with no flags, nothing is armed. Without it, "the
// injection worked" and "the injection is always on" look the same.
func TestTheUnsetSwitchesLeaveTheHealthyShape(t *testing.T) {
	in, err := parseInject(t)
	if err != nil {
		t.Fatalf("parse with no arguments: %v", err)
	}
	if !in.empty() {
		t.Errorf("an empty command line armed something: %v", in.lines())
	}
	if got := in.describe(); !strings.Contains(got, "nothing") {
		t.Errorf("the banner should say nothing is injected, got %q", got)
	}

	stub := clienttest.New()
	in.apply(stub)
	if got := stub.Balance(); got == 0 {
		t.Errorf("balance is 0 with no switch armed; the healthy fixture balance is not zero")
	}
}

// TestEachSwitchAloneTurnsOffTheHealthyClaim. The banner's whole job is to say
// whether the healthy shape is in front of the walker, so "one armed switch is
// enough to stop claiming health" is the property, and it has to hold for every
// switch INDIVIDUALLY.
//
// This test exists because a mutation showed it was missing: making empty()
// ignore -balance alone left every other test green, because they either pass no
// flags at all or pass many. A switch that arms the stub but leaves the banner
// saying "nothing - this is the healthy shape" is the worst version of this
// whole file: the walker is told the screen is correct while it is injected.
func TestEachSwitchAloneTurnsOffTheHealthyClaim(t *testing.T) {
	cases := []struct {
		arg  string
		want string
	}{
		{"-balance=0", "balance"},
		{"-model-ineligible=buding-cloud-fast", "model withdrawn"},
		{"-fail-ledger=502", "ledger read"},
		{"-fail-bootstrap=502", "bootstrap"},
		{"-fail-completions=402", "gateway turn"},
		{"-fail-logout=503", "logout"},
		{"-omit-ledger-balance", "ledger balance"},
		{"-catalog-version=2026-01-01.1", "catalog version"},
		{"-catalog-ttl=60", "catalog ttl"},
		{"-policy-audience=other-product", "policy audience"},
		{"-expire-access-tokens", "access tokens"},
		{"-omit-policy", "policy envelope"},
		{"-tamper-policy", "policy payload"},
	}

	for _, tc := range cases {
		t.Run(tc.arg, func(t *testing.T) {
			in, err := parseInject(t, tc.arg)
			if err != nil {
				t.Fatalf("parse %s: %v", tc.arg, err)
			}
			if in.empty() {
				t.Errorf("%s is armed but the switches read as empty, so the banner will claim health", tc.arg)
			}
			got := in.describe()
			if strings.Contains(got, "nothing") {
				t.Errorf("%s is armed but the banner says nothing is injected:\n%s", tc.arg, got)
			}
			if !strings.Contains(got, tc.want) {
				t.Errorf("%s is armed but the banner does not mention %q:\n%s", tc.arg, tc.want, got)
			}
		})
	}
}

// TestZeroIsNotTheSameAsUnset is the reason optionalInt64 exists rather than
// flag.Int64. -balance=0 is a walkthrough step in its own right (L-C4b ②: an
// entry point that must stay visible at zero credit), so "0" and "not given"
// have to be two different states — and a zero-valued default collapses them.
func TestZeroIsNotTheSameAsUnset(t *testing.T) {
	unset, err := parseInject(t)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if !unset.balance.unset() {
		t.Error("no -balance given, but the switch reads as set")
	}

	zero, err := parseInject(t, "-balance=0")
	if err != nil {
		t.Fatalf("parse -balance=0: %v", err)
	}
	if zero.balance.unset() {
		t.Fatal("-balance=0 reads as unset, so a zero balance cannot be injected")
	}
	if zero.balance.value != 0 {
		t.Errorf("-balance=0 parsed as %d", zero.balance.value)
	}

	stub := clienttest.New()
	zero.apply(stub)
	if got := stub.Balance(); got != 0 {
		t.Errorf("the ledger reports %d after -balance=0; the switch did not reach the stub", got)
	}
}

// TestTheBannerNamesEveryArmedSwitch. The banner is what the walker reads to
// decide whether the screen in front of them is wrong; a switch that is armed
// but unlisted turns the walkthrough into a debugging session, which is the
// failure printFixtures exists to prevent for the tool switch.
func TestTheBannerNamesEveryArmedSwitch(t *testing.T) {
	in, err := parseInject(t,
		"-balance=0",
		"-model-ineligible=buding-cloud-fast",
		"-fail-ledger=502",
		"-fail-completions=402:insufficient_credits",
		"-fail-bootstrap=502:upstream_unavailable",
		"-fail-logout=503",
		"-omit-ledger-balance",
		"-catalog-version=2026-01-01.1",
		"-catalog-ttl=60",
		"-policy-audience=other-product",
		"-expire-access-tokens",
		"-omit-policy",
		"-tamper-policy",
	)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	got := in.describe()
	for _, want := range []string{
		"balance", "0 micro-credits",
		"model withdrawn", "buding-cloud-fast",
		"ledger read", "502",
		"gateway turn", "402 insufficient_credits",
		"bootstrap", "502 upstream_unavailable",
		"logout", "503",
		"ledger balance",
		"catalog version", "2026-01-01.1",
		"catalog ttl", "60s",
		"policy audience", "other-product",
		"access tokens",
		"policy envelope",
		"policy payload",
	} {
		if !strings.Contains(got, want) {
			t.Errorf("the banner does not mention %q:\n%s", want, got)
		}
	}
	if strings.Contains(got, "nothing") {
		t.Errorf("the banner claims nothing is injected while switches are armed:\n%s", got)
	}
}

// TestTheModelWithdrawalKeepsTheEntryAndCutsEligibility. The distinction is the
// whole point of the switch (L-C7): a model the platform takes away is still IN
// the catalog, and the client's projection is what turns eligible=false into
// "pick another model". Removing the entry instead would exercise a different
// path and would not prove the filter ran.
func TestTheModelWithdrawalKeepsTheEntryAndCutsEligibility(t *testing.T) {
	in, err := parseInject(t, "-model-ineligible=buding-cloud-fast")
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	stub := clienttest.New()
	in.apply(stub)

	env, err := stub.Policy()
	if err != nil {
		t.Fatalf("sign the fixture policy: %v", err)
	}
	policy, err := env.DecodePolicy()
	if err != nil {
		t.Fatalf("decode the signed policy: %v", err)
	}

	byID := map[string]bool{}
	for _, m := range policy.Catalog.Models {
		byID[m.ID] = m.Eligible
	}
	if len(byID) != 3 {
		t.Fatalf("the catalog lost an entry: %v", byID)
	}
	if eligible, ok := byID["buding-cloud-fast"]; !ok {
		t.Fatalf("the withdrawn model is absent from the catalog rather than ineligible: %v", byID)
	} else if eligible {
		t.Errorf("buding-cloud-fast is still eligible")
	}
	for _, id := range []string{"buding-privacy-1", "buding-cloud-pro"} {
		if !byID[id] {
			t.Errorf("%s was withdrawn too; the switch names exactly one model", id)
		}
	}
}

// TestTheFailureSwitchesReachTheWire. The banner saying "every read fails with
// 502" is a claim about the stand-in, and this asserts the claim end to end: a
// real sign-in against the real handler, then the ledger route. A banner that
// lies is worse than no banner, because the walker trusts it — and "the flag was
// parsed" is not the same fact as "the request failed".
func TestTheFailureSwitchesReachTheWire(t *testing.T) {
	cases := []struct {
		name    string
		args    []string
		want    int
		wantNil bool
	}{
		{name: "the healthy shape answers the ledger", want: http.StatusOK},
		{name: "-fail-ledger reaches the route", args: []string{"-fail-ledger=502"}, want: http.StatusBadGateway},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			in, err := parseInject(t, tc.args...)
			if err != nil {
				t.Fatalf("parse %v: %v", tc.args, err)
			}
			stub := clienttest.New()
			in.apply(stub)

			handler := stub.Handler()
			token := signIn(t, handler)

			rec := httptest.NewRecorder()
			req := httptest.NewRequest(http.MethodGet, "/v1/credits/ledger", nil)
			req.Header.Set("Authorization", "Bearer "+token)
			handler.ServeHTTP(rec, req)

			if rec.Code != tc.want {
				t.Errorf("ledger status = %d, want %d: %s", rec.Code, tc.want, rec.Body.String())
			}
		})
	}
}

// TestBadSwitchValuesAreRefused. A switch that silently ignores a typo is worse
// than one that fails: the walker would read the healthy shape as "the flow is
// broken". The status range is checked because 0 is clienttest's own "no
// injection" value — accepting it would switch an injection off while the
// command line says it is on.
func TestBadSwitchValuesAreRefused(t *testing.T) {
	for _, args := range [][]string{
		{"-fail-ledger=abc"},
		{"-fail-ledger=0"},
		{"-fail-ledger=999"},
		{"-fail-ledger=99"},
		{"-balance=-1"},
		{"-balance=abc"},
		{"-catalog-ttl=-5"},
		{"-catalog-ttl=abc"},
	} {
		if _, err := parseInject(t, args...); err == nil {
			t.Errorf("%v was accepted; it should be refused with a message naming the expected form", args)
		}
	}
}

// TestTheStatusCodePairIsOptionalOnBothHalves. A bare status is a real case, not
// a convenience: a proxy answering 502 sends no business code and the client
// falls back to the transport tier. Deriving a code from the status would put a
// second copy of the status↔code mapping in this binary, while
// internal/productclient/testdata/wire-error-codes.txt is its only owner.
func TestTheStatusCodePairIsOptionalOnBothHalves(t *testing.T) {
	bare, err := parseInject(t, "-fail-ledger=502")
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if bare.failLedger.status != 502 || bare.failLedger.code != "" {
		t.Errorf("bare status parsed as %d/%q", bare.failLedger.status, bare.failLedger.code)
	}
	if got := bare.failLedger.String(); got != "502" {
		t.Errorf("String() = %q, want 502", got)
	}

	paired, err := parseInject(t, "-fail-ledger=402:insufficient_credits")
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if paired.failLedger.status != 402 || paired.failLedger.code != "insufficient_credits" {
		t.Errorf("paired value parsed as %d/%q", paired.failLedger.status, paired.failLedger.code)
	}
	if got := paired.failLedger.String(); got != "402:insufficient_credits" {
		t.Errorf("String() = %q", got)
	}
}

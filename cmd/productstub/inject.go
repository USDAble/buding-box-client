// Fault-injection switches for the hand-run stand-in.
//
// WHY THIS FILE EXISTS (手工走查单.md §3). Three walkthrough steps had to edit Go
// source and restart the stand-in to reach their branch: L-C7 ② (withdraw a
// model), L-C4a ②④ (move the balance, fail the ledger read) and L-C4b ② (a zero
// balance). Every one of those states already had a setter on clienttest.Server
// — the setters exist because the automated tests need them — but nothing on the
// command line could reach them, so a hand walkthrough was editing the same
// source the tests assert on and hoping the edit was faithful.
//
// The cost was not "inconvenient". A walkthrough step that requires a source edit
// is not repeatable: the edit is forgotten, or it is subtly different the second
// time, and the walker cannot tell "the switch is on" from "the flow is broken".
// That is the same failure mode withRequestLog exists to close for "was the
// stand-in even reached", and printFixtures for "is the tool switch on".
//
// WHY FLAGS RATHER THAN A LOOPBACK ENDPOINT. A walkthrough is re-run by finding
// the same command in the shell history, so the switches have to be in the
// command. An endpoint would put the state somewhere the banner cannot read, and
// then the banner — which is what the walker actually looks at — would no longer
// be able to say what is injected.
//
// WHY ALL OF THEM DEFAULT TO OFF. A stand-in that always fails is not a healthy
// fixture, and a walkthrough that cannot see the healthy shape cannot tell a
// regression from its own injection. This is the same rule every setter in
// clienttest carries.
//
// SCOPE (开发规范 §3.10): developer, local, run-time only. These switches reach
// nothing that ships — see the package comment in main.go.
package main

import (
	"flag"
	"fmt"
	"strconv"
	"strings"

	"github.com/open-octo/octo-agent/internal/productclient/clienttest"
)

// inject holds every switch this binary exposes. It is a plain struct rather
// than a pile of flag pointers because two things read it: the code that arms
// the stand-in, and the banner that says what is armed. One field, one place.
type inject struct {
	balance optionalInt64

	failBootstrap   failure
	failLedger      failure
	failCompletions failure
	failLogout      failure

	catalogVersion  string
	catalogTTL      optionalInt
	policyAudience  string
	ineligibleModel string

	expireAccessTokens bool
	omitPolicy         bool
	tamperPolicy       bool
	omitLedgerBalance  bool
}

// registerInject declares the switches. Keeping the declarations together with
// the struct means a new switch cannot be added to one and forgotten in the
// other.
func registerInject(fs *flag.FlagSet, in *inject) {
	fs.Var(&in.balance, "balance", "make the ledger report this many micro-credits (0 is the interesting one: a user out of credit); unset means the fixture balance")
	fs.Var(&in.failLedger, "fail-ledger", "make every ledger read fail, as <status>[:<code>] e.g. 502 (a proxy) or 402:insufficient_credits (the platform)")
	fs.Var(&in.failBootstrap, "fail-bootstrap", "make every bootstrap fail, as <status>[:<code>] e.g. 502:upstream_unavailable")
	fs.Var(&in.failCompletions, "fail-completions", "make every gateway turn fail, as <status>[:<code>] e.g. 402:insufficient_credits")
	fs.Var(&in.failLogout, "fail-logout", "make every logout fail, as <status>[:<code>] e.g. 503 (the platform session is then not revoked, PQ29)")
	fs.StringVar(&in.catalogVersion, "catalog-version", "", "serve this catalog version; combine with a login that already cached a newer one for the downgrade defence")
	fs.Var(&in.catalogTTL, "catalog-ttl", "make the fixture policy claim this freshness window in seconds")
	fs.StringVar(&in.policyAudience, "policy-audience", "", "address the policy to this audience instead of the fixture's, i.e. a policy for another product")
	fs.StringVar(&in.ineligibleModel, "model-ineligible", "", "mark this catalog model eligible=false, i.e. a model the platform has taken away (buding-cloud-fast, ...)")
	fs.BoolVar(&in.expireAccessTokens, "expire-access-tokens", false, "reject every already-issued access token, leaving refresh tokens valid")
	fs.BoolVar(&in.omitPolicy, "omit-policy", false, "answer bootstrap with no policy envelope at all")
	fs.BoolVar(&in.tamperPolicy, "tamper-policy", false, "sign correctly, then change one byte of the payload")
	fs.BoolVar(&in.omitLedgerBalance, "omit-ledger-balance", false, "answer the ledger without balanceMicroCredits (distinguishes absent from zero)")
}

// empty reports whether nothing is injected, which is the healthy shape.
func (in inject) empty() bool {
	return in.balance.unset() &&
		in.failBootstrap.unset() && in.failLedger.unset() &&
		in.failCompletions.unset() && in.failLogout.unset() &&
		in.catalogVersion == "" && in.catalogTTL.unset() &&
		in.policyAudience == "" && in.ineligibleModel == "" &&
		!in.expireAccessTokens && !in.omitPolicy &&
		!in.tamperPolicy && !in.omitLedgerBalance
}

// apply arms the stand-in. Every switch is armed before Serve starts, so the
// first request already sees the injected state — arming lazily on first request
// would make the opening request healthy and hide a broken switch.
func (in inject) apply(stub *clienttest.Server) {
	if !in.balance.unset() {
		stub.SetBalance(in.balance.value)
	}
	if !in.failBootstrap.unset() {
		stub.FailBootstrap(in.failBootstrap.status, in.failBootstrap.code)
	}
	if !in.failLedger.unset() {
		stub.FailLedger(in.failLedger.status, in.failLedger.code)
	}
	if !in.failCompletions.unset() {
		stub.FailCompletions(in.failCompletions.status, in.failCompletions.code)
	}
	if !in.failLogout.unset() {
		stub.FailLogout(in.failLogout.status, in.failLogout.code)
	}
	if in.catalogVersion != "" {
		stub.SetCatalogVersion(in.catalogVersion)
	}
	if !in.catalogTTL.unset() {
		stub.SetCatalogTTL(in.catalogTTL.value)
	}
	if in.policyAudience != "" {
		stub.SetPolicyAudience(in.policyAudience)
	}
	if in.ineligibleModel != "" {
		stub.SetModelIneligible(in.ineligibleModel)
	}
	if in.expireAccessTokens {
		stub.ExpireAccessTokens()
	}
	if in.omitPolicy {
		stub.OmitPolicy()
	}
	if in.tamperPolicy {
		stub.TamperPolicy()
	}
	if in.omitLedgerBalance {
		stub.OmitLedgerBalance()
	}
}

// describe renders the armed switches for the banner.
//
// WHY IT PRINTS AT ALL, INCLUDING WHEN EMPTY. The walker reads the banner to
// decide what the screen in front of them is supposed to show. A banner that
// says nothing about injections leaves "the ledger read is broken" and "the
// ledger read is meant to fail" looking identical, and the walkthrough turns
// into a debugging session — the reasoning printFixtures already carries for the
// tool switch.
func (in inject) describe() string {
	if in.empty() {
		return "  injected     nothing - this is the healthy shape"
	}
	var out strings.Builder
	out.WriteString("  injected     NOT the healthy shape:")
	for _, l := range in.lines() {
		out.WriteString("\n                 ")
		out.WriteString(l)
	}
	return out.String()
}

// lines renders one entry per armed switch. Ordered by what a walkthrough
// reaches for first rather than alphabetically: the balance and the model list
// are the two a person moves most often.
func (in inject) lines() []string {
	var out []string
	add := func(name, detail string) {
		out = append(out, fmt.Sprintf("%-19s %s", name, detail))
	}
	if !in.balance.unset() {
		add("balance", fmt.Sprintf("%d micro-credits, overriding the fixture's healthy value", in.balance.value))
	}
	if in.ineligibleModel != "" {
		add("model withdrawn", in.ineligibleModel+" - still in the catalog, eligible=false")
	}
	if !in.failLedger.unset() {
		add("ledger read", in.failLedger.describe("read"))
	}
	if !in.failBootstrap.unset() {
		add("bootstrap", in.failBootstrap.describe("attempt"))
	}
	if !in.failCompletions.unset() {
		add("gateway turn", in.failCompletions.describe("turn"))
	}
	if !in.failLogout.unset() {
		add("logout", in.failLogout.describe("attempt"))
	}
	if in.omitLedgerBalance {
		add("ledger balance", "omitted entirely - absent, not zero")
	}
	if in.catalogVersion != "" {
		add("catalog version", in.catalogVersion)
	}
	if !in.catalogTTL.unset() {
		add("catalog ttl", strconv.Itoa(in.catalogTTL.value)+"s")
	}
	if in.policyAudience != "" {
		add("policy audience", in.policyAudience)
	}
	if in.expireAccessTokens {
		add("access tokens", "every issued token rejected - refresh still works")
	}
	if in.omitPolicy {
		add("policy envelope", "omitted from bootstrap")
	}
	if in.tamperPolicy {
		add("policy payload", "signed, then one byte changed")
	}
	return out
}

// failure is a <status>[:<code>] switch: the HTTP status, and optionally the
// business code the contract's error envelope carries.
//
// WHY THE STATUS IS THE SWITCH AND THE CODE IS OPTIONAL. A switch is armed when
// its status is non-zero, matching every Fail* setter on clienttest.Server (the
// rule and its history are on FailBootstrap). A bare status is therefore a real
// case rather than a convenience: a proxy answering 502 carries no business code,
// and the client falls back to the transport tier's wording — catalogTransport
// exists for exactly that (需求基线 B4). Deriving a code from the status is the
// one thing this must not do, because the status↔code registry is owned by
// internal/productclient/testdata/wire-error-codes.txt (开发规范 §3.8), and a
// second copy here would drift.
type failure struct {
	status int
	code   string
}

func (f *failure) String() string {
	if f.status == 0 {
		return ""
	}
	if f.code == "" {
		return strconv.Itoa(f.status)
	}
	return strconv.Itoa(f.status) + ":" + f.code
}

func (f *failure) Set(raw string) error {
	status, code, _ := strings.Cut(raw, ":")
	n, err := strconv.Atoi(strings.TrimSpace(status))
	if err != nil {
		return fmt.Errorf("want <status>[:<code>], got %q: the status is not a number", raw)
	}
	// A zero status is the stand-in's own "no injection" value (every setter in
	// clienttest reads it that way), so accepting it would silently switch the
	// injection off while the command line says it is on.
	if n < 100 || n > 599 {
		return fmt.Errorf("want an HTTP status in 100..599, got %d", n)
	}
	f.status = n
	f.code = strings.TrimSpace(code)
	return nil
}

func (f failure) unset() bool { return f.status == 0 }

func (f failure) describe(what string) string {
	if f.code == "" {
		return fmt.Sprintf("every %s fails with %d and no business code", what, f.status)
	}
	return fmt.Sprintf("every %s fails with %d %s", what, f.status, f.code)
}

// optionalInt64 is a flag that distinguishes "not given" from "given as zero",
// which flag.Int64 cannot: -balance=0 is the whole point of one walkthrough
// step, and a zero-valued default would make it indistinguishable from silence.
type optionalInt64 struct {
	set   bool
	value int64
}

func (o *optionalInt64) String() string {
	if !o.set {
		return ""
	}
	return strconv.FormatInt(o.value, 10)
}

func (o *optionalInt64) Set(raw string) error {
	n, err := strconv.ParseInt(strings.TrimSpace(raw), 10, 64)
	if err != nil {
		return fmt.Errorf("want a whole number of micro-credits, got %q", raw)
	}
	if n < 0 {
		return fmt.Errorf("micro-credits cannot be negative, got %d", n)
	}
	o.set, o.value = true, n
	return nil
}

func (o *optionalInt64) unset() bool { return !o.set }

// optionalInt is the same idea for the catalog's ttlSec, where 0 is a meaningful
// value the fixture reads as "use the default".
type optionalInt struct {
	set   bool
	value int
}

func (o *optionalInt) String() string {
	if !o.set {
		return ""
	}
	return strconv.Itoa(o.value)
}

func (o *optionalInt) Set(raw string) error {
	n, err := strconv.Atoi(strings.TrimSpace(raw))
	if err != nil {
		return fmt.Errorf("want a whole number of seconds, got %q", raw)
	}
	if n < 0 {
		return fmt.Errorf("seconds cannot be negative, got %d", n)
	}
	o.set, o.value = true, n
	return nil
}

func (o *optionalInt) unset() bool { return !o.set }

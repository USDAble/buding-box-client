// OCTO-FORK: turn refusals that name the control plane's error code (G3) — see
// dev-docs-usdable/需求/20260911/开发计划.md §PR-5d3.
package server

import "fmt"

// codeModelWithdrawn is the client-side fail-closed code for "this session's model
// is not in the catalog we hold" (PR-5e's refusal). It is registered in the
// client-local codes of 中台交付包 §3.2 — the ONE registry for code values, which the
// frontend's mapping table (web/src/lib/turnError.ts) reads it from.
//
// WHY A CONST RATHER THAN THE STRING INLINE. The value crosses into JavaScript and
// the two ends are pinned by two literals (this const's test on one side,
// turnError.test.ts's literal on the other) because no build step can compare Go to
// TS. Keeping it in one place at least makes the Go half greppable.
const codeModelWithdrawn = "model_withdrawn"

// codedRefusal is a turn refusal that also names a control-plane code.
//
// The sentence stays because it is what logs and no-UI builds show, and because the
// requirement wants the user to be told what to do — but it is NOT what the browser
// renders: C8 rule 1 forbids the client from using `message` as copy, so the code is
// the only road from here to a localized sentence.
//
// It implements agent.CodedError by shape, which is how internal/server hands a code
// up without importing a vendor adapter (开发规范 §2).
type codedRefusal struct {
	code string
	msg  string
}

func (c codedRefusal) Error() string     { return c.msg }
func (c codedRefusal) ErrorCode() string { return c.code }

// errModelNotListed is PR-5e's refusal (L-C7): the session is bound to a catalog
// model the catalog no longer offers, so there is nothing to send to and no model
// may be substituted (B8 forbids the silent switch).
//
// It says "pick one from the list" rather than "this model was withdrawn" because
// internal/server cannot see whether the list still has models in it, and a user
// whose catalog came back empty needs a different sentence from one whose single
// model was retired. Both of those have owners — the distinction in
// internal/productruntime, the wording in the frontend's i18n, for which this is
// the backstop when there is no UI (开发计划 §PR-5e).
//
// It lives in this file rather than server.go because a coded refusal is fork-owned
// machinery, and server.go is under a churn ceiling (server-diff-guard R2).
func errModelNotListed(model string) error {
	return codedRefusal{
		code: codeModelWithdrawn,
		msg: fmt.Sprintf(
			"model %q is no longer in the model list this build was given — pick one from the list; the turn was not started and nothing was sent",
			model),
	}
}

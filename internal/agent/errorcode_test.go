package agent

import (
	"errors"
	"fmt"
	"testing"
)

// codeErr stands in for the two producers of platform error codes: internal/provider
// (what the endpoint answered) and internal/server (what this build refused before
// sending anything). The agent itself only ever sees the interface — it must not
// learn the vendor spellings, and neither producer may import the other (开发规范 §2).
type codeErr struct {
	code string
	msg  string
}

func (e codeErr) Error() string     { return e.msg }
func (e codeErr) ErrorCode() string { return e.code }

// TestErrorCodeOfReadsThroughWrapping: the code is SET three frames below where it
// is READ (provider sets it, the agent loop wraps it, internal/server puts it on the
// wire), and the loop wraps with "agent: loop[0]: %w" (agent.go). Without the unwrap,
// the code would survive a unit test that hands the error straight to ErrorCodeOf and
// vanish on the real path — which is the shape of V-57 (a field that existed and that
// nothing on the live path could see).
func TestErrorCodeOfReadsThroughWrapping(t *testing.T) {
	err := fmt.Errorf("agent: loop[0]: %w",
		fmt.Errorf("openai: HTTP 402: %w", codeErr{code: "insufficient_credits", msg: "HTTP 402"}))

	if got := ErrorCodeOf(err); got != "insufficient_credits" {
		t.Errorf("ErrorCodeOf = %q, want %q — the code has to survive the loop's %%w wrapping, or the frontend gets no code at all",
			got, "insufficient_credits")
	}
}

// TestErrorCodeOfIsEmptyWhenThereIsNoCode pins the other half: "carries no code" must
// be distinguishable from "carries the empty string", because the frontend keys its
// copy on that difference — a turn_error WITHOUT `code` falls back to the server's
// sentence, and one with an empty code must not be treated as a known code that
// happens to have no copy.
func TestErrorCodeOfIsEmptyWhenThereIsNoCode(t *testing.T) {
	if got := ErrorCodeOf(errors.New("upstream exploded")); got != "" {
		t.Errorf("ErrorCodeOf(plain error) = %q, want the empty string", got)
	}
	if got := ErrorCodeOf(nil); got != "" {
		t.Errorf("ErrorCodeOf(nil) = %q, want the empty string", got)
	}
	if got := ErrorCodeOf(fmt.Errorf("agent: loop[0]: %w", errors.New("boom"))); got != "" {
		t.Errorf("ErrorCodeOf(wrapped plain error) = %q, want the empty string", got)
	}
}

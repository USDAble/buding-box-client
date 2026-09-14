// OCTO-FORK: the platform's error-code channel (需求基线 C8 / G3) — see
// dev-docs-usdable/需求/20260911/开发计划.md §PR-5d3.
package agent

import "errors"

// CodedError is implemented by errors that carry the control plane's error code
// (中台交付包 §3.2). The code is the only thing the UI copy may key on: C8 forbids
// rendering `message`, so an error that names a code is how the layers that know it
// hand it up to the layer that puts it on the wire.
//
// The interface lives HERE rather than in either producer because the two of them
// must not know about each other (开发规范 §2): internal/provider knows what the
// endpoint answered, internal/server knows what this build refused before sending,
// and both already import this package.
type CodedError interface {
	error
	ErrorCode() string
}

// ErrorCodeOf returns the platform code carried by err, or "" when it carries none.
// It unwraps, because the agent loop and the senders wrap with %w — the code is set
// three frames below where it is read.
//
// "" means "no code", which is a fact the frontend acts on (it falls back to the
// server's sentence). It is not a code whose copy happens to be missing, and the two
// must not be collapsed.
func ErrorCodeOf(err error) string {
	var ce CodedError
	if errors.As(err, &ce) {
		return ce.ErrorCode()
	}
	return ""
}

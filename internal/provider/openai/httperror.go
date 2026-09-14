// OCTO-FORK: the control plane's error envelope alongside OpenAI's — see
// dev-docs-usdable/需求/20260911/开发计划.md §PR-5d3.
package openai

import (
	"encoding/json"
	"fmt"
)

// HTTPError is a non-2xx answer from the endpoint.
//
// WHY IT IS A TYPE AND NOT A fmt.Errorf. Two different things travel out of a
// failed request, and they have different audiences:
//
//   - Code — the control plane's error code (中台交付包 §3.2). It is the ONLY thing
//     the client's UI copy may key on (需求基线 C8 rule 1: "客户端只按 code … 映射
//     UI，不能解析 message"). The frontend maps it to a localized sentence; this
//     adapter must carry it without interpreting it.
//   - Text — the sentence logs and no-UI builds show. It is built HERE, once, so the
//     wording for a body this adapter understands does not drift between the two
//     call sites (client.go's buffered path and stream.go's streaming one).
//
// Both shapes the endpoint can answer with are recognised: OpenAI's nested
// {"error":{"message","type","code"}} and the control plane's flat
// {"code","message",…}. A body in neither shape stays a code-less error whose Text
// is the raw body — nothing is invented, and the frontend falls back to Text.
type HTTPError struct {
	Status int
	// Kind is the label rendered in parentheses in Text: OpenAI's `error.type`, or
	// the flat envelope's code. Display only — read Code for the machine half.
	Kind string
	// Text is the human-readable half: the envelope's message, or the raw body when
	// the body did not parse.
	Text string
	// Code is the control plane's error code, "" when the body named none.
	Code string
}

func (e *HTTPError) Error() string {
	if e.Kind != "" {
		return fmt.Sprintf("openai: HTTP %d (%s): %s", e.Status, e.Kind, e.Text)
	}
	return fmt.Sprintf("openai: HTTP %d: %s", e.Status, e.Text)
}

// ErrorCode implements agent.CodedError.
func (e *HTTPError) ErrorCode() string { return e.Code }

// httpErrorFromBody builds the error for a non-2xx answer.
//
// The nested shape is tried first because it is the one this adapter was written for
// and the one every non-gateway endpoint in this repo answers with; a flat envelope
// has no top-level "error" key, so the first attempt leaves Message empty and the
// second one applies. Neither match ⇒ raw body, no code.
//
// The flat envelope's `message` is deliberately NOT dropped: the requirement is that
// the USER sees a localized sentence, which happens in the browser via the code, and
// the message is what the fallback (a log, or a build with no UI) shows instead of a
// wall of JSON.
func httpErrorFromBody(status int, body []byte) error {
	var apiErr apiError
	if err := json.Unmarshal(body, &apiErr); err == nil && apiErr.Error.Message != "" {
		return &HTTPError{
			Status: status,
			Kind:   apiErr.Error.Type,
			Text:   apiErr.Error.Message,
			Code:   apiErr.Error.Code,
		}
	}
	var flat struct {
		Code    string `json:"code"`
		Message string `json:"message"`
	}
	// Code OR message: the contract calls `message` human-readable and required in its
	// example, but a refusal that only names its code is still a usable answer, and
	// treating it as unparseable would put the JSON back in front of the user — the one
	// outcome L-C4c exists to prevent. A code with no sentence falls back to the code
	// itself, which is at least what the platform decided.
	if err := json.Unmarshal(body, &flat); err == nil && (flat.Code != "" || flat.Message != "") {
		text := flat.Message
		if text == "" {
			text = flat.Code
		}
		return &HTTPError{
			Status: status,
			Kind:   flat.Code,
			Text:   text,
			Code:   flat.Code,
		}
	}
	return &HTTPError{Status: status, Text: string(body)}
}

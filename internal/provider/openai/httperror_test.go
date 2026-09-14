package openai

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/open-octo/octo-agent/internal/agent"
	"github.com/open-octo/octo-agent/internal/provider"
)

// request is the minimal request both Send and SendStream accept.
func request() provider.Request {
	return provider.Request{Model: "x", Messages: []agent.Message{agent.NewUserMessage("hi")}}
}

// TestSendStream_PlatformEnvelopeBecomesACodeNotRawJSON is L-C4c's nail at the layer
// that mangles it. 中台交付包 §3.2 registers the error envelope as FLAT —
// {"code","message","field","retryAfterSec","requestId"} — while this adapter was
// written for OpenAI's NESTED shape ({"error":{"message","type","code"}}). A body in
// the flat shape parsed to nothing, so it fell through to the last line of the old
// code and the user's bubble read:
//
//	openai: HTTP 402: {"code":"insufficient_credits","message":"…","requestId":"gw_1"}
//
// i.e. English raw JSON where the requirement asks for one Chinese sentence whose
// owner is the frontend's i18n (§3.8). The code is what carries that across; the
// sentence in the body is what the fallback shows when nobody knows the code.
func TestSendStream_PlatformEnvelopeBecomesACodeNotRawJSON(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusPaymentRequired)
		_, _ = w.Write([]byte(`{"code":"insufficient_credits","message":"balance too low","retryAfterSec":60,"requestId":"gw_1"}`))
	}))
	defer srv.Close()

	c, _ := New("k")
	c.BaseURL = srv.URL

	_, err := c.SendStream(context.Background(), request(), provider.StreamCallbacks{})
	if err == nil {
		t.Fatal("SendStream returned nil for a 402 — the gateway refuses to serve the turn, so this must not look like a successful empty reply")
	}

	var ce agent.CodedError
	if !errors.As(err, &ce) {
		t.Fatalf("err = %v (%T) carries no code; the frontend can only key its copy on `code`, so the user gets this sentence instead of 余额不足", err, err)
	}
	if ce.ErrorCode() != "insufficient_credits" {
		t.Errorf("code = %q, want %q (中台交付包 §3.2's spelling; the client must not invent or translate it)", ce.ErrorCode(), "insufficient_credits")
	}
	if !strings.Contains(err.Error(), "balance too low") {
		t.Errorf("error = %q, want the envelope's message — it is the fallback sentence for a build with no UI", err.Error())
	}
	if strings.Contains(err.Error(), "{") || strings.Contains(err.Error(), `"code"`) {
		t.Errorf("error = %q still carries the raw JSON body; L-C4c is exactly about that not reaching the user", err.Error())
	}
}

// TestSendStream_NestedPlatformCodeIsReadToo: the gateway speaks OpenAI, so a nested
// body is a second legitimate spelling of the same fact — and it is the shape every
// non-gateway endpoint in this repo already uses (client_test.go's 401 case). Reading
// the code from either shape keeps one code channel instead of two.
func TestSendStream_NestedPlatformCodeIsReadToo(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusPaymentRequired)
		_, _ = w.Write([]byte(`{"error":{"message":"insufficient credits","type":"invalid_request_error","code":"insufficient_credits"}}`))
	}))
	defer srv.Close()

	c, _ := New("k")
	c.BaseURL = srv.URL

	_, err := c.SendStream(context.Background(), request(), provider.StreamCallbacks{})
	var ce agent.CodedError
	if !errors.As(err, &ce) || ce.ErrorCode() != "insufficient_credits" {
		t.Fatalf("err = %v, code = %q; want insufficient_credits from the nested shape as well", err, agent.ErrorCodeOf(err))
	}
}

// TestSend_OpenAIShapedErrorKeepsItsSentence is the anti-regression half: the sentence
// the header-compatible endpoints produced before this change must not move. The code
// is additive — the message is still what logs and no-UI builds show.
func TestSend_OpenAIShapedErrorKeepsItsSentence(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
		_, _ = w.Write([]byte(`{"error":{"message":"Incorrect API key","type":"invalid_request_error","code":"invalid_api_key"}}`))
	}))
	defer srv.Close()

	c, _ := New("k")
	c.BaseURL = srv.URL

	_, err := c.Send(context.Background(), request())
	if err == nil {
		t.Fatal("Send returned nil for a 401")
	}
	if !strings.Contains(err.Error(), "HTTP 401") {
		t.Errorf("error = %q, want it to mention HTTP 401 (unchanged wording)", err.Error())
	}
	if !strings.Contains(err.Error(), "Incorrect API key") {
		t.Errorf("error = %q, want the upstream message (unchanged wording)", err.Error())
	}
	var ce agent.CodedError
	if !errors.As(err, &ce) || ce.ErrorCode() != "invalid_api_key" {
		t.Errorf("code = %q, want invalid_api_key — a body that names a code should hand it over", agent.ErrorCodeOf(err))
	}
}

// TestSend_UnparseableBodyKeepsTheOldSentence: when the body is not an error envelope
// at all (a proxy's HTML, an empty 502), nothing is invented — same sentence as
// before, and no code, so the frontend falls back to it instead of claiming to know
// which failure this was.
func TestSend_UnparseableBodyKeepsTheOldSentence(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusBadGateway)
		_, _ = w.Write([]byte("upstream exploded"))
	}))
	defer srv.Close()

	c, _ := New("k")
	c.BaseURL = srv.URL

	_, err := c.Send(context.Background(), request())
	if err == nil {
		t.Fatal("Send returned nil for a 502")
	}
	if want := "openai: HTTP 502: upstream exploded"; err.Error() != want {
		t.Errorf("error = %q, want %q", err.Error(), want)
	}
	if got := agent.ErrorCodeOf(err); got != "" {
		t.Errorf("code = %q, want the empty string — an unparseable body names no code", got)
	}
}

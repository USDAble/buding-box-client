package clienttest

import (
	"bufio"
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// These tests pin the stand-in gateway added for PR-5a. It exists so the turn
// path can be walked by hand against the same fixture the automated tests use,
// which means two properties matter more than the reply text: the turn is
// refused without a token (or the fixture would hide a sender that never
// presents one), and the reply arrives split across chunks (or the aggregation
// the provider does would go unexercised in the one place a person can see it).

// postTurn sends one completions request and returns the response plus its body.
func postTurn(t *testing.T, base, token, body string) (*http.Response, string) {
	t.Helper()
	req, err := http.NewRequest(http.MethodPost, base+"/v1/chat/completions", strings.NewReader(body))
	if err != nil {
		t.Fatalf("build request: %v", err)
	}
	req.Header.Set("Content-Type", "application/json")
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("post turn: %v", err)
	}
	defer resp.Body.Close()
	var buf bytes.Buffer
	if _, err := buf.ReadFrom(resp.Body); err != nil {
		t.Fatalf("read turn body: %v", err)
	}
	return resp, buf.String()
}

// streamedContent reassembles what an OpenAI-protocol client would see: every
// `data:` frame's delta concatenated, ignoring the terminator.
func streamedContent(t *testing.T, body string) string {
	t.Helper()
	var out strings.Builder
	scanner := bufio.NewScanner(strings.NewReader(body))
	for scanner.Scan() {
		line := scanner.Text()
		payload, ok := strings.CutPrefix(line, "data: ")
		if !ok || payload == "[DONE]" {
			continue
		}
		var frame struct {
			Choices []struct {
				Delta struct {
					Content string `json:"content"`
				} `json:"delta"`
			} `json:"choices"`
		}
		if err := json.Unmarshal([]byte(payload), &frame); err != nil {
			t.Fatalf("frame is not OpenAI-shaped JSON: %v (%s)", err, payload)
		}
		if len(frame.Choices) > 0 {
			out.WriteString(frame.Choices[0].Delta.Content)
		}
	}
	if err := scanner.Err(); err != nil {
		t.Fatalf("scan stream: %v", err)
	}
	return out.String()
}

func TestATurnWithoutATokenIsRefused(t *testing.T) {
	standin := New()
	ts := httptest.NewServer(standin.Handler())
	defer ts.Close()

	resp, body := postTurn(t, ts.URL, "", `{"model":"buding-privacy-1","stream":true,"messages":[{"role":"user","content":"hi"}]}`)

	if resp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("status = %d, want 401; body = %s", resp.StatusCode, body)
	}
	// It dialed and was refused *here*, which is the distinction that lets a
	// test tell this apart from a client that refused to dial at all (B4).
	if got := standin.CompletionCount(); got != 1 {
		t.Errorf("CompletionCount = %d, want 1 — the refusal must happen at the fixture, not before the request", got)
	}
}

func TestASignedInTurnStreamsAndNamesTheBareModelID(t *testing.T) {
	standin := New()
	ts := httptest.NewServer(standin.Handler())
	defer ts.Close()

	token := loginForBootstrap(t, ts.URL)

	// The bare catalog id, not the composite session id: the gateway is what
	// strips the endpoint prefix. Seeing it echoed is how a hand test confirms
	// that PR-4d's composite id was unwrapped before the wire.
	const model = "buding-privacy-1"
	resp, body := postTurn(t, ts.URL, token,
		`{"model":"`+model+`","stream":true,"messages":[{"role":"user","content":"hi"}]}`)

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want 200; body = %s", resp.StatusCode, body)
	}
	if ct := resp.Header.Get("Content-Type"); !strings.HasPrefix(ct, "text/event-stream") {
		t.Errorf("Content-Type = %q, want text/event-stream — the provider decides to aggregate from this", ct)
	}
	if !strings.Contains(body, "data: [DONE]") {
		t.Error("stream has no [DONE] terminator")
	}

	content := streamedContent(t, body)
	if !strings.Contains(content, model) {
		t.Errorf("reply does not name the model it was asked for: %q", content)
	}
	if !strings.Contains(content, "1 message(s) received") {
		t.Errorf("reply does not report the request it saw: %q", content)
	}
	// More than one frame means the content really was chunked, so the
	// reassembly above is doing something.
	if frames := strings.Count(body, "chat.completion.chunk"); frames < 2 {
		t.Errorf("reply arrived in %d chunk(s); the fixture must chunk so aggregation is exercised", frames)
	}
}

func TestATurnWithoutStreamingAlsoAnswers(t *testing.T) {
	// The product streams, but the provider falls back to the non-streaming call
	// when streaming is disabled. A fixture that only spoke SSE would turn that
	// into a silent hang, which is the worst kind of dead end to debug by hand.
	standin := New()
	ts := httptest.NewServer(standin.Handler())
	defer ts.Close()

	token := loginForBootstrap(t, ts.URL)
	resp, body := postTurn(t, ts.URL, token, `{"model":"buding-privacy-1","stream":false,"messages":[]}`)

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want 200; body = %s", resp.StatusCode, body)
	}
	var out struct {
		Choices []struct {
			Message struct {
				Content string `json:"content"`
			} `json:"message"`
		} `json:"choices"`
	}
	if err := json.Unmarshal([]byte(body), &out); err != nil {
		t.Fatalf("body is not an OpenAI completion: %v (%s)", err, body)
	}
	if len(out.Choices) == 0 || out.Choices[0].Message.Content == "" {
		t.Errorf("no assistant message in %s", body)
	}
}

func TestARotatedAwayTokenStopsWorkingAtTheGateway(t *testing.T) {
	// The gateway validates against the live session table rather than accepting
	// any non-empty string. Without this, a hand test could "work" with a token
	// the control plane had already dropped, and C2's in-memory refresh would
	// look unnecessary.
	standin := New()
	ts := httptest.NewServer(standin.Handler())
	defer ts.Close()

	loginForBootstrap(t, ts.URL)
	standin.ExpireAccessTokens()

	resp, body := postTurn(t, ts.URL, "at_999", `{"model":"buding-privacy-1","stream":true,"messages":[]}`)
	if resp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("status = %d, want 401 for a token the platform no longer knows; body = %s", resp.StatusCode, body)
	}
}

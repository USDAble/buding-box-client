package openai

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"testing"

	"github.com/open-octo/octo-agent/internal/agent"
	"github.com/open-octo/octo-agent/internal/provider"
)

// D-002's safety net. The control plane's gateway speaks OpenAI-compatible
// chat/completions, but `retract` (withdrawn content) and the ledger terminal
// state are NOT part of that protocol. A `data: {"retract":{...}}` line has no
// `choices`, so it used to be skipped in silence: the user kept reading content
// the platform had withdrawn and nothing anywhere said so.
//
// The net is deliberately a REPORT, not a handler — the protocol for either event
// is still unpinned (待解决问题.md D-002 / G1). Its only job is to make sure that
// when the gateway first sends one, we find out immediately instead of quietly
// losing it (开发规范 §3.9: 明确失败, not 静默失效).

// streamingWith serves one SSE body and runs a minimal request against it.
func streamingWith(t *testing.T, body string, cb provider.StreamCallbacks) (provider.Response, error) {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		w.WriteHeader(http.StatusOK)
		_, _ = io.WriteString(w, body)
		if f, ok := w.(http.Flusher); ok {
			f.Flush()
		}
	}))
	t.Cleanup(srv.Close)

	c, err := New("test-key")
	if err != nil {
		t.Fatal(err)
	}
	c.BaseURL = srv.URL
	return c.SendStream(context.Background(), provider.Request{
		Model:    "gpt-4o-mini",
		Messages: []agent.Message{agent.NewUserMessage("hi")},
	}, cb)
}

func TestSendStream_ReportsUnmodelledChunkFields(t *testing.T) {
	// The gateway's withdrawn-content event, exactly as 待解决问题.md D-002
	// describes it: a top-level field that is not in the chunk schema.
	body := `data: {"id":"c1","object":"chat.completion.chunk","choices":[{"index":0,"delta":{"content":"text the platform withdrew"}}]}` + "\n\n" +
		`data: {"retract":{"reason":"safety_blocked"}}` + "\n\n" +
		"data: [DONE]\n\n"

	var reported []string
	resp, err := streamingWith(t, body, provider.StreamCallbacks{
		OnUnmodelledChunkFields: func(fields []string) { reported = append(reported, fields...) },
	})
	if err != nil {
		t.Fatalf("SendStream: %v", err)
	}

	// The turn still completes — the net must not break a reply mid-stream. This
	// is the difference between "loud" and "fatal": the retract semantics are
	// unimplemented, so failing the stream would be inventing a protocol.
	if resp.Content != "text the platform withdrew" {
		t.Errorf("Content = %q, want the content that streamed before the unknown field", resp.Content)
	}

	if !reflect.DeepEqual(reported, []string{"retract"}) {
		t.Fatalf("reported = %v, want [retract]: an unmodelled field was dropped in silence (D-002)", reported)
	}
}

func TestSendStream_KnowsEveryFieldRealOpenAIEmits(t *testing.T) {
	// The reason the net can be strict: a FALSE POSITIVE here would make real
	// traffic look broken. These are the fields an OpenAI-compatible server sends
	// on every reply, including the ones this client does no work with
	// (`created`, `system_fingerprint`).
	body := `data: {"id":"c1","object":"chat.completion.chunk","created":1767225600,"model":"gpt-4o-mini","system_fingerprint":"fp_44709d6fcb","choices":[{"index":0,"delta":{"content":"hi"}}]}` + "\n\n" +
		`data: {"id":"c1","object":"chat.completion.chunk","created":1767225600,"model":"gpt-4o-mini","system_fingerprint":"fp_44709d6fcb","choices":[{"index":0,"delta":{},"finish_reason":"stop"}]}` + "\n\n" +
		"data: [DONE]\n\n"

	var reported []string
	if _, err := streamingWith(t, body, provider.StreamCallbacks{
		OnUnmodelledChunkFields: func(fields []string) { reported = append(reported, fields...) },
	}); err != nil {
		t.Fatalf("SendStream: %v", err)
	}
	if len(reported) != 0 {
		t.Errorf("reported = %v, want none: these fields are standard, not unmodelled", reported)
	}
}

func TestUnmodelledChunkFields(t *testing.T) {
	cases := []struct {
		name string
		data string
		want []string
	}{
		{"a standard chunk models every key", `{"id":"c1","object":"chat.completion.chunk","created":1,"model":"m","system_fingerprint":"fp","choices":[],"usage":{},"error":null}`, nil},
		{"an unknown key is named", `{"id":"c1","retract":{"reason":"safety"}}`, []string{"retract"}},
		{"the report is sorted so two runs agree", `{"zeta":1,"alpha":2,"model":"m"}`, []string{"alpha", "zeta"}},
		{"unparseable bytes report nothing", `{"id":`, nil},
		{"an empty object is not an unknown field", `{}`, nil},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := unmodelledChunkFields([]byte(tc.data))
			if !reflect.DeepEqual(got, tc.want) {
				t.Errorf("unmodelledChunkFields(%s) = %v, want %v", tc.data, got, tc.want)
			}
		})
	}
}

// knownChunkFields must be derived from the struct tags, not maintained beside
// them: a hand-kept list of the same fact is a second owner of it, and it drifts
// the first time a field is added (开发规范 §3.8). This pins the derivation, so
// adding a field to streamChunk is automatically known without touching the net.
func TestKnownChunkFields_CoversEveryStructTag(t *testing.T) {
	typ := reflect.TypeOf(streamChunk{})
	for i := 0; i < typ.NumField(); i++ {
		tag := typ.Field(i).Tag.Get("json")
		name, _, _ := strings.Cut(tag, ",")
		if name == "" || name == "-" {
			continue
		}
		if _, ok := knownChunkFields[name]; !ok {
			t.Errorf("streamChunk field %q (json %q) is not in knownChunkFields: a standard field would be reported as unknown", typ.Field(i).Name, name)
		}
	}
}

// A server that sends an unknown field must not break the stream for a caller
// that supplies no callback — the report is optional, the reply is not.
func TestSendStream_UnknownFieldsWithoutCallbackStillSucceeds(t *testing.T) {
	body := `data: {"id":"c1","choices":[{"index":0,"delta":{"content":"ok"}}]}` + "\n\n" +
		`data: {"retract":{"reason":"safety_blocked"}}` + "\n\n" +
		"data: [DONE]\n\n"

	resp, err := streamingWith(t, body, provider.StreamCallbacks{})
	if err != nil {
		t.Fatalf("SendStream: %v", err)
	}
	if resp.Content != "ok" {
		t.Errorf("Content = %q, want %q", resp.Content, "ok")
	}
}

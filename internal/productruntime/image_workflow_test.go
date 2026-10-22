package productruntime

import (
	"context"
	"encoding/json"
	"fmt"
	"image"
	"image/png"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"github.com/open-octo/octo-agent/internal/agent"
	"github.com/open-octo/octo-agent/internal/app"
	"github.com/open-octo/octo-agent/internal/sensitive"
	"github.com/open-octo/octo-agent/internal/tools"
)

// Exercise the actual HTTP sender, streaming loop and read_file together: the
// first request succeeds, and an optional image check must not make the next
// request fail or silently change its tools, model or effort.
func TestImageCheckDoesNotAbortCompletedArtifact(t *testing.T) {
	f, model := imageCatalogFixture(t)
	path := filepath.Join(t.TempDir(), "poster.png")
	file, err := os.Create(path)
	if err != nil {
		t.Fatal(err)
	}
	if err := png.Encode(file, image.NewRGBA(image.Rect(0, 0, 24, 36))); err != nil {
		t.Fatal(err)
	}
	if err := file.Close(); err != nil {
		t.Fatal(err)
	}
	original, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	arguments, _ := json.Marshal(map[string]string{"path": path})
	var mu sync.Mutex
	requests := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		defer mu.Unlock()
		requests++
		var body struct {
			Model           string            `json:"model"`
			ReasoningEffort string            `json:"reasoning_effort"`
			Tools           []json.RawMessage `json:"tools"`
			Messages        json.RawMessage   `json:"messages"`
		}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Error(err)
			http.Error(w, "invalid request", http.StatusBadRequest)
			return
		}
		if body.Model != model || body.ReasoningEffort != "default" || len(body.Tools) != 1 {
			t.Errorf("request silently changed: model=%s effort=%s tools=%d", body.Model, body.ReasoningEffort, len(body.Tools))
		}
		if strings.Contains(string(body.Messages), `"image_url"`) {
			http.Error(w, `{"error":{"message":"CAPABILITY_NOT_VERIFIED"}}`, http.StatusUnprocessableEntity)
			return
		}
		w.Header().Set("Content-Type", "text/event-stream")
		delta := map[string]any{"content": "File ready. Visual inspection was not performed; image/tool combination is not verified."}
		finish := "stop"
		if requests == 1 {
			delta = map[string]any{"tool_calls": []any{map[string]any{
				"index": 0, "id": "read-poster", "type": "function",
				"function": map[string]any{"name": "read_file", "arguments": string(arguments)},
			}}}
			finish = "tool_calls"
		} else {
			for _, expected := range []string{"capability_not_verified", "24x36", "NOT performed"} {
				if !strings.Contains(string(body.Messages), expected) {
					t.Errorf("tool feedback omitted %q: %s", expected, body.Messages)
				}
			}
		}
		chunk, _ := json.Marshal(map[string]any{
			"id": "image-workflow", "object": "chat.completion.chunk", "model": model,
			"choices": []any{map[string]any{"index": 0, "delta": delta, "finish_reason": finish}},
		})
		fmt.Fprintf(w, "data: %s\n\ndata: [DONE]\n\n", chunk)
	}))
	defer server.Close()
	gateway := GatewayEndpoint{
		Host: server.URL, Tokens: signedIn(f.login.AccessToken),
		ReasoningForModel: func(string) (string, error) { return "default", nil },
		ValidateReasoning: f.rt.ValidateModelReasoning,
		ImageInputStatus:  f.rt.ModelImageInputStatus,
	}
	sender, err := gateway.Sender(app.ReasoningTuning{ClientModelID: model})
	if err != nil {
		t.Fatal(err)
	}
	a := agent.New(app.WrapSensitive(sender, sensitive.New("")), model)
	ctx := tools.WithModelVision(context.Background(), true)
	reply, err := a.RunStream(ctx, "Deliver the existing poster, inspect it if possible.", []agent.ToolDefinition{(tools.ReadFileTool{}).Definition()}, tools.NewDefaultRegistry(), nil)
	if err != nil || !strings.Contains(reply.Content, "File ready") {
		t.Fatalf("completed artifact was blocked: %+v %v", reply, err)
	}
	mu.Lock()
	count := requests
	mu.Unlock()
	if count != 2 {
		t.Fatalf("requests=%d; wanted a tool call and final response", count)
	}
	stored, err := os.ReadFile(path)
	if err != nil || string(stored) != string(original) {
		t.Fatal("image check changed the deliverable", err)
	}
}

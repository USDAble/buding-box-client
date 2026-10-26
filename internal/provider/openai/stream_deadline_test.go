package openai

import (
	"context"
	"errors"
	"fmt"
	"github.com/open-octo/octo-agent/internal/agent"
	"github.com/open-octo/octo-agent/internal/provider"
	"github.com/open-octo/octo-agent/internal/provider/retry"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

func deadlineRequest() provider.Request {
	return provider.Request{Model: "test", Messages: []agent.Message{agent.NewUserMessage("synthetic")}}
}

func TestStreamHeaderDeadline(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.Copy(io.Discard, r.Body)
		<-r.Context().Done()
	}))
	defer srv.Close()
	c, _ := New("test")
	c.BaseURL = srv.URL
	c.StreamHeaderTimeout = 30 * time.Millisecond
	c.Retry = retry.Policy{MaxAttempts: 1}
	_, err := c.SendStream(context.Background(), deadlineRequest(), provider.StreamCallbacks{})
	if !errors.Is(err, context.DeadlineExceeded) || !strings.Contains(err.Error(), "response headers") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestStreamTailDeadlineNeverInventsCompletion(t *testing.T) {
	for _, usage := range []bool{false, true} {
		t.Run(fmt.Sprint(usage), func(t *testing.T) {
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				_, _ = io.Copy(io.Discard, r.Body)
				w.Header().Set("Content-Type", "text/event-stream")
				fmt.Fprint(w, "data: {\"choices\":[{\"delta\":{\"content\":\"ready\"},\"finish_reason\":\"stop\"}]}\n\n")
				if usage {
					fmt.Fprint(w, "data: {\"choices\":[],\"usage\":{\"prompt_tokens\":7,\"completion_tokens\":2}}\n\n")
				}
				w.(http.Flusher).Flush()
				<-r.Context().Done()
			}))
			defer srv.Close()
			c, _ := New("test")
			c.BaseURL = srv.URL
			c.StreamTailTimeout = 30 * time.Millisecond
			_, err := c.SendStream(context.Background(), deadlineRequest(), provider.StreamCallbacks{})
			if err == nil || !strings.Contains(err.Error(), "completion tail") {
				t.Fatalf("missing framing accepted: %v", err)
			}
			var transient interface{ TransientStream() bool }
			if errors.As(err, &transient) {
				t.Fatal("terminal tail failure must not automatically replay paid generation")
			}
		})
	}
}

func TestStreamDelayedUsageAndToolArgumentsSurviveTail(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.Copy(io.Discard, r.Body)
		w.Header().Set("Content-Type", "text/event-stream")
		fmt.Fprint(w, "data: {\"choices\":[{\"delta\":{\"tool_calls\":[{\"index\":0,\"id\":\"call1\",\"function\":{\"name\":\"read_file\",\"arguments\":\"{\\\"path\\\":\\\"x\\\"}\"}}]},\"finish_reason\":\"tool_calls\"}]}\n\n")
		w.(http.Flusher).Flush()
		time.Sleep(40 * time.Millisecond)
		fmt.Fprint(w, "data: {\"choices\":[],\"usage\":{\"prompt_tokens\":7,\"completion_tokens\":2}}\n\ndata: [DONE]\n\n")
	}))
	defer srv.Close()
	c, _ := New("test")
	c.BaseURL = srv.URL
	c.StreamHeaderTimeout = 20 * time.Millisecond
	c.StreamTailTimeout = time.Second
	resp, err := c.SendStream(context.Background(), deadlineRequest(), provider.StreamCallbacks{})
	if err != nil {
		t.Fatal(err)
	}
	if resp.InputTokens != 7 || resp.OutputTokens != 2 || len(resp.Blocks) != 1 || resp.Blocks[0].Input["path"] != "x" {
		t.Fatalf("lost usage or tool arguments: %+v", resp)
	}
}

func TestStreamTailCallerCancellation(t *testing.T) {
	sent := make(chan struct{})
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.Copy(io.Discard, r.Body)
		fmt.Fprint(w, "data: {\"choices\":[{\"delta\":{},\"finish_reason\":\"stop\"}]}\n\n")
		w.(http.Flusher).Flush()
		close(sent)
		<-r.Context().Done()
	}))
	defer srv.Close()
	c, _ := New("test")
	c.BaseURL = srv.URL
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go func() { <-sent; cancel() }()
	_, err := c.SendStream(ctx, deadlineRequest(), provider.StreamCallbacks{})
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("cancel became retry/tail error: %v", err)
	}
}

type tailAgentSender struct{ client *Client }

func (s tailAgentSender) SendMessages(ctx context.Context, model, system string, messages []agent.Message, maxTokens int) (agent.Reply, error) {
	return s.SendMessagesWithTools(ctx, model, system, messages, maxTokens, nil)
}
func (s tailAgentSender) SendMessagesWithTools(ctx context.Context, model, system string, messages []agent.Message, maxTokens int, tools []agent.ToolDefinition) (agent.Reply, error) {
	r, err := s.client.SendStream(ctx, provider.Request{Model: model, SystemPrompt: system, Messages: messages, MaxTokens: maxTokens, Tools: tools}, provider.StreamCallbacks{})
	return agent.Reply{Content: r.Content, Blocks: r.Blocks, StopReason: r.StopReason}, err
}

type tailAgentExecutor struct{ calls int }

func (e *tailAgentExecutor) Execute(context.Context, string, map[string]any) (agent.ToolResult, error) {
	e.calls++
	return agent.ToolResult{}, nil
}

func TestStreamTailDeadlineDoesNotReplayAgentRoundOrDispatchTools(t *testing.T) {
	var requests atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.Copy(io.Discard, r.Body)
		requests.Add(1)
		fmt.Fprint(w, "data: {\"choices\":[{\"delta\":{\"tool_calls\":[{\"index\":0,\"id\":\"call1\",\"function\":{\"name\":\"read_file\",\"arguments\":\"{}\"}}]},\"finish_reason\":\"tool_calls\"}]}\n\n")
		w.(http.Flusher).Flush()
		<-r.Context().Done()
	}))
	defer srv.Close()
	c, _ := New("test")
	c.BaseURL = srv.URL
	c.StreamTailTimeout = 20 * time.Millisecond
	a := agent.New(tailAgentSender{c}, "test")
	e := &tailAgentExecutor{}
	_, err := a.Run(context.Background(), "synthetic", []agent.ToolDefinition{{Name: "read_file"}}, e)
	if err == nil || !strings.Contains(err.Error(), "completion tail") || requests.Load() != 1 || e.calls != 0 {
		t.Fatalf("tail timeout repeated work: error=%v requests=%d toolcalls=%d", err, requests.Load(), e.calls)
	}
}

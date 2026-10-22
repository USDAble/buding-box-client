package server

import (
	"context"
	"github.com/open-octo/octo-agent/internal/agent"
	"testing"
	"time"
)

type cancelSuggestionSender struct {
	started chan struct{}
	stopped chan struct{}
}

func (s cancelSuggestionSender) SendMessages(ctx context.Context, _, _ string, _ []agent.Message, _ int) (agent.Reply, error) {
	close(s.started)
	<-ctx.Done()
	close(s.stopped)
	// A misbehaving adapter's late success still must never reach a newer turn.
	return agent.Reply{Content: "obsolete"}, nil
}

func TestSuggestionCanceledByNewForegroundTurn(t *testing.T) {
	s := &Server{wsHub: &wsHub{events: make(chan wsEventEnvelope, 10)}}
	sender := cancelSuggestionSender{make(chan struct{}), make(chan struct{})}
	a := agent.New(sender, "unchanged-model")
	a.History.Append(agent.NewUserMessage("synthetic"))
	old := s.beginSuggestionTurn("session")
	ctx, cancel, ok := s.claimSuggestion("session", old)
	if !ok {
		t.Fatal("new suggestion rejected")
	}
	defer cancel()
	completed := make(chan struct{})
	go func() {
		defer close(completed)
		_, _ = a.Suggest(ctx, nil)
		if release := s.claimSuggestionPublication("session", old, ctx); release != nil {
			defer release()
			s.wsHub.broadcast("session", "obsolete")
		}
	}()
	select {
	case <-sender.started:
	case <-time.After(time.Second):
		t.Fatal("suggestion did not start")
	}
	current := s.beginSuggestionTurn("session")
	select {
	case <-sender.stopped:
	case <-time.After(time.Second):
		t.Fatal("old request was not canceled")
	}
	<-completed
	if release := s.claimSuggestionPublication("session", old, context.Background()); release != nil {
		release()
		t.Fatal("late generation allowed")
	}
	if len(s.wsHub.events) != 0 {
		t.Fatal("obsolete suggestion published")
	}
	release := s.claimSuggestionPublication("session", current, context.Background())
	if release == nil {
		t.Fatal("current publication rejected")
	}
	s.wsHub.broadcast("session", map[string]any{"text": "current"})
	release()
	event := <-s.wsHub.events
	if event.Event.(map[string]any)["text"] != "current" {
		t.Fatal("wrong suggestion")
	}
	// Completion consumes the generation, so a duplicate completion cannot overwrite it.
	if release := s.claimSuggestionPublication("session", current, context.Background()); release != nil {
		release()
		t.Fatal("duplicate publication allowed")
	}
	if len(s.wsHub.events) != 0 {
		t.Fatal("duplicate suggestion published")
	}
}

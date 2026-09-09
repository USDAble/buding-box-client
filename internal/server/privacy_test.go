package server

import (
	"context"
	"path/filepath"
	"testing"

	"github.com/open-octo/octo-agent/internal/agent"
	"github.com/open-octo/octo-agent/internal/app"
	"github.com/open-octo/octo-agent/internal/chatmode"
	"github.com/open-octo/octo-agent/internal/productstate"
)

type privacyProbeSender struct {
	messages []agent.Message
}

func (p *privacyProbeSender) SendMessages(_ context.Context, _, _ string, messages []agent.Message, _ int) (agent.Reply, error) {
	p.messages = messages
	return agent.Reply{}, nil
}

func TestWithSessionPrivacy(t *testing.T) {
	privacyByDefault := productStateWithDefaultMode(t, chatmode.ModePrivacy)

	tests := []struct {
		name string
		srv  *Server
		sess *agent.Session
		want string
	}{
		{
			name: "explicit privacy",
			srv:  &Server{},
			sess: &agent.Session{ChatMode: chatmode.ModePrivacy},
			want: "联系我 138****8000",
		},
		{
			name: "explicit smart overrides account default",
			srv:  &Server{productState: privacyByDefault},
			sess: &agent.Session{ChatMode: chatmode.ModeSmart},
			want: "联系我 13812348000",
		},
		{
			name: "legacy empty mode inherits account default",
			srv:  &Server{productState: privacyByDefault},
			sess: &agent.Session{},
			want: "联系我 138****8000",
		},
		{
			name: "nil session inherits default mode",
			srv:  &Server{productState: privacyByDefault},
			want: "联系我 138****8000",
		},
		{
			name: "missing product state falls back to default group",
			srv:  &Server{},
			sess: &agent.Session{},
			want: "联系我 13812348000",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			probe := &privacyProbeSender{}
			sender := app.WrapPII(probe)
			ctx := tt.srv.withSessionPrivacy(context.Background(), tt.sess)
			_, err := sender.SendMessages(ctx, "model", "", []agent.Message{agent.NewUserMessage("联系我 13812348000")}, 0)
			if err != nil {
				t.Fatal(err)
			}
			if got := probe.messages[0].Content; got != tt.want {
				t.Fatalf("provider content = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestRunTurnMasksProviderCopyAndKeepsSessionHistory(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv("HOME", tmp)
	t.Setenv("OCTO_DATA_ROOT", tmp)
	t.Setenv("USERPROFILE", tmp)

	probe := &privacyProbeSender{}
	srv := &Server{
		cfg:    Config{Tools: false},
		model:  "stub-model",
		cwd:    tmp,
		sender: app.WrapPII(probe),
	}
	sess := agent.NewSession("stub-model", "")
	sess.ChatMode = chatmode.ModePrivacy

	if _, err := srv.runTurn(context.Background(), sess, "联系我 138-0000-1234"); err != nil {
		t.Fatalf("runTurn: %v", err)
	}
	if got := probe.messages[len(probe.messages)-1].Content; got != "联系我 138-****-1234" {
		t.Fatalf("provider content = %q", got)
	}
	if got := sess.Messages[0].Content; got != "联系我 138-0000-1234" {
		t.Fatalf("in-memory history = %q", got)
	}
	if err := sess.Save(); err != nil {
		t.Fatal(err)
	}
	reloaded, err := agent.LoadSession(sess.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got := reloaded.Messages[0].Content; got != "联系我 138-0000-1234" {
		t.Fatalf("persisted history = %q", got)
	}
}

func productStateWithDefaultMode(t *testing.T, mode string) *productstate.Store {
	t.Helper()
	store, err := productstate.Open(filepath.Join(t.TempDir(), "product-state.json"))
	if err != nil {
		t.Fatal(err)
	}
	if err := store.Mutate(func(st *productstate.State) error {
		st.Prefs.DefaultChatMode = mode
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	return store
}

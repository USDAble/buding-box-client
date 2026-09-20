package tools

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/open-octo/octo-agent/internal/agent"
	"github.com/open-octo/octo-agent/internal/permission"
)

func TestPlatformSkillAvailabilityAndFailure(t *testing.T) {
	ctx := context.Background()
	for _, d := range DefaultToolsForCtx(ctx, "test") {
		if d.Name == "load_platform_skill" {
			t.Fatal("unwired tool advertised")
		}
	}
	result, err := (PlatformSkillTool{}).Execute(ctx, "", map[string]any{"skill_id": "x"})
	if err == nil || result.Text != "" {
		t.Fatal("unwired loader reported success")
	}
	ctx = WithPlatformSkillLoader(ctx, func(context.Context, string) (string, error) {
		return "must not leak", errors.New("version unavailable")
	})
	result, err = (PlatformSkillTool{}).Execute(ctx, "", map[string]any{"skill_id": "x"})
	if err == nil || result.Text != "" {
		t.Fatal("failed load returned instructions")
	}
	gate, err := permission.New("", t.TempDir(), permission.ModeStrict)
	if err != nil {
		t.Fatal(err)
	}
	if gate.Check("load_platform_skill", map[string]any{"skill_id": "x"}) != permission.Allow {
		t.Fatal("read-only instruction loader blocked")
	}
}

type platformTaskSender struct {
	path  string
	calls int
}

func (s *platformTaskSender) SendMessages(context.Context, string, string, []agent.Message, int) (agent.Reply, error) {
	return agent.Reply{}, errors.New("tool path required")
}
func (s *platformTaskSender) SendMessagesWithTools(_ context.Context, _ string, _ string, history []agent.Message, _ int, _ []agent.ToolDefinition) (agent.Reply, error) {
	s.calls++
	switch s.calls {
	case 1:
		return agent.Reply{StopReason: "tool_use", Blocks: []agent.ContentBlock{agent.NewToolUseBlock("load-1", "load_platform_skill", map[string]any{"skill_id": "meeting-v1"})}}, nil
	case 2:
		found := false
		for _, m := range history {
			for _, b := range m.Blocks {
				if b.Type == "tool_result" && !b.IsError && strings.Contains(b.Result, "Record decisions and owners") {
					found = true
				}
			}
		}
		if !found {
			return agent.Reply{}, errors.New("model did not receive loaded skill")
		}
		return agent.Reply{StopReason: "tool_use", Blocks: []agent.ContentBlock{agent.NewToolUseBlock("write-1", "write_file", map[string]any{"path": s.path, "content": "Decision: ship\nOwner: Alice\n"})}}, nil
	default:
		return agent.Reply{StopReason: "end_turn", Content: "Meeting notes saved."}, nil
	}
}

// OCTO-FORK: an isolated test artifact proves a loaded skill can drive the
// actual agent/tool loop; provider intelligence is deliberately a fixture.
func TestPlatformSkillAgentProducesArtifact(t *testing.T) {
	path := filepath.Join(t.TempDir(), "meeting.md")
	sender := &platformTaskSender{path: path}
	a := agent.New(sender, "fixture")
	ctx := WithPlatformSkillLoader(context.Background(), func(_ context.Context, id string) (string, error) {
		if id != "meeting-v1" {
			return "", errors.New("skill not bound to expert")
		}
		return "Record decisions and owners", nil
	})
	_, err := a.Run(ctx, "Produce meeting notes", []agent.ToolDefinition{(PlatformSkillTool{}).Definition(), (WriteFileTool{}).Definition()}, NewDefaultRegistry())
	if err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(path)
	if err != nil || string(data) != "Decision: ship\nOwner: Alice\n" {
		t.Fatalf("real artifact missing: %q %v", data, err)
	}
	if sender.calls != 3 {
		t.Fatalf("tool loop calls=%d", sender.calls)
	}
}

func TestPlatformSkillFailedLoadDoesNotProduceArtifact(t *testing.T) {
	path := filepath.Join(t.TempDir(), "must-not-exist.md")
	sender := &platformTaskSender{path: path}
	a := agent.New(sender, "fixture")
	ctx := WithPlatformSkillLoader(context.Background(), func(context.Context, string) (string, error) {
		return "", errors.New("pinned skill unavailable")
	})
	_, err := a.Run(ctx, "Produce meeting notes", []agent.ToolDefinition{(PlatformSkillTool{}).Definition(), (WriteFileTool{}).Definition()}, NewDefaultRegistry())
	if err == nil {
		t.Fatal("failed skill load led to successful task")
	}
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Fatalf("unexpected task artifact: %v", err)
	}
}

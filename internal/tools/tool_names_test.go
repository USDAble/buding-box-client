package tools

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/open-octo/octo-agent/internal/agent"
)

type casingSender struct {
	blocks []agent.ContentBlock
	calls  int
}

func (s *casingSender) SendMessages(context.Context, string, string, []agent.Message, int) (agent.Reply, error) {
	panic("expected tool-aware sender")
}

func (s *casingSender) SendMessagesWithTools(context.Context, string, string, []agent.Message, int, []agent.ToolDefinition) (agent.Reply, error) {
	s.calls++
	if s.calls == 1 {
		return agent.Reply{StopReason: "tool_use", Blocks: s.blocks}, nil
	}
	return agent.Reply{StopReason: "end_turn", Content: "done"}, nil
}

func TestAgent_CapitalizedSkillAndGlobUseRealRegistry(t *testing.T) {
	setSkillsFor(t, "cron-task-creator", "---\ndescription: schedule tasks\n---\nCollect the schedule and prompt before creating a task.")
	dir := t.TempDir()
	path := filepath.Join(dir, "task.txt")
	if err := os.WriteFile(path, []byte("fixture"), 0o600); err != nil {
		t.Fatal(err)
	}
	sender := &casingSender{blocks: []agent.ContentBlock{
		agent.NewToolUseBlock("load", "Skill", map[string]any{"name": "cron-task-creator"}),
		agent.NewToolUseBlock("find", "Glob", map[string]any{"path": dir, "pattern": "*.txt"}),
	}}
	a := agent.New(sender, "fixture")
	_, err := a.RunStream(context.Background(), "/cron-task-creator", []agent.ToolDefinition{
		SkillTool{}.Definition(), GlobTool{}.Definition(),
	}, NewDefaultRegistry(), nil)
	if err != nil {
		t.Fatal(err)
	}
	results := map[string]string{}
	for _, message := range a.History.Snapshot() {
		for _, block := range message.Blocks {
			if block.Type == "tool_result" {
				if block.IsError {
					t.Fatalf("%s failed: %s", block.ToolUseID, block.Result)
				}
				results[block.ToolUseID] = block.Result
			}
		}
	}
	if !strings.Contains(results["load"], "Collect the schedule") || !strings.Contains(results["find"], "task.txt") {
		t.Fatalf("skill body or glob result missing: %v", results)
	}
}

func TestAgent_CapitalizedWriteStillRequiresRead(t *testing.T) {
	path := filepath.Join(t.TempDir(), "existing.txt")
	if err := os.WriteFile(path, []byte("keep"), 0o600); err != nil {
		t.Fatal(err)
	}
	sender := &casingSender{blocks: []agent.ContentBlock{
		agent.NewToolUseBlock("write", "Write_File", map[string]any{"path": path, "content": "overwrite"}),
	}}
	a := agent.New(sender, "fixture")
	_, err := a.Run(context.Background(), "write", []agent.ToolDefinition{WriteFileTool{}.Definition()}, NewDefaultRegistry())
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, message := range a.History.Snapshot() {
		for _, block := range message.Blocks {
			if block.Type == "tool_result" {
				found = block.IsError && strings.Contains(block.Result, "not been read")
			}
		}
	}
	if !found {
		t.Fatal("capitalized write did not reach read-before-write enforcement")
	}
	if data, err := os.ReadFile(path); err != nil || string(data) != "keep" {
		t.Fatalf("unread file changed: %q %v", data, err)
	}
}

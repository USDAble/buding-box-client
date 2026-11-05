package tools

import (
	"context"
	"reflect"
	"strings"
	"testing"

	"github.com/open-octo/octo-agent/internal/agent"
)

func TestSkillTool_NameAndSkillArguments(t *testing.T) {
	setSkillsFor(t, "greet", "---\ndescription: say hi\n---\nStep 1: be nice.")
	for _, tc := range []struct {
		name  string
		input map[string]any
		error string
	}{
		{"name", map[string]any{"name": "greet"}, ""},
		{"skill alias", map[string]any{"skill": "greet"}, ""},
		{"name takes precedence", map[string]any{"name": "greet", "skill": "ghost"}, ""},
		{"unknown name stays unknown", map[string]any{"name": "ghost", "skill": "greet"}, "unknown skill"},
		{"empty name stays invalid", map[string]any{"name": "", "skill": "greet"}, "name is required"},
		{"nonstring name stays invalid", map[string]any{"name": 42, "skill": "greet"}, "name is required"},
		{"missing", nil, "name is required"},
		{"empty alias", map[string]any{"skill": ""}, "name is required"},
		{"nonstring alias", map[string]any{"skill": 42}, "name is required"},
		{"unknown alias", map[string]any{"skill": "ghost"}, "unknown skill"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			before := make(map[string]any, len(tc.input))
			for k, v := range tc.input {
				before[k] = v
			}
			out, err := (SkillTool{}).Execute(context.Background(), "skill", tc.input)
			if tc.error != "" {
				if err == nil || !strings.Contains(err.Error(), tc.error) {
					t.Fatalf("error = %v, want %q", err, tc.error)
				}
			} else if err != nil || !strings.Contains(out.Text, "Step 1: be nice.") {
				t.Fatalf("load = %q, %v", out.Text, err)
			}
			if tc.input != nil && !reflect.DeepEqual(tc.input, before) {
				t.Fatalf("tool mutated the recorded arguments: %v", tc.input)
			}
		})
	}
}

func TestSkillTool_SkillArgumentKeepsProfileRestrictions(t *testing.T) {
	setSkillsFor(t, "greet", "---\ndescription: say hi\n---\nSay hi.")
	if _, err := (SkillTool{}).Execute(expertCtx(t, nil), "skill", map[string]any{"skill": "greet"}); err == nil || !strings.Contains(err.Error(), "not enabled for this agent") {
		t.Fatalf("ungranted alias = %v", err)
	}
	setSkillsFor(t, "mgmt", "---\ndescription: manage\nsystem: true\n---\nManage.")
	if _, err := (SkillTool{}).Execute(expertCtx(t, []string{"mgmt"}), "skill", map[string]any{"skill": "mgmt"}); err == nil || !strings.Contains(err.Error(), "system skill") {
		t.Fatalf("system alias = %v", err)
	}
}

func TestAgent_CreatorSkillArgumentPreservesHistory(t *testing.T) {
	for _, name := range []string{"skill-creator", "expert-agent-manager"} {
		t.Run(name, func(t *testing.T) {
			setSkillsFor(t, name, "---\ndescription: create\nsystem: true\n---\nAsk what the user wants to create.")
			input := map[string]any{"skill": name}
			sender := &casingSender{blocks: []agent.ContentBlock{
				agent.NewToolUseBlock("load", "skill", input),
			}}
			a := agent.New(sender, "fixture")
			_, err := a.RunStream(context.Background(), "/"+name, []agent.ToolDefinition{SkillTool{}.Definition()}, NewDefaultRegistry(), nil)
			if err != nil {
				t.Fatal(err)
			}
			loaded := false
			for _, msg := range a.History.Snapshot() {
				for _, block := range msg.Blocks {
					if block.Type == "tool_use" && !reflect.DeepEqual(block.Input, map[string]any{"skill": name}) {
						t.Fatalf("history arguments changed: %v", block.Input)
					}
					if block.Type == "tool_result" {
						loaded = block.ToolUseID == "load" && !block.IsError && strings.Contains(block.Result, "Ask what the user wants to create.")
					}
				}
			}
			if !loaded || sender.calls != 2 {
				t.Fatalf("creator setup did not continue with the loaded skill: loaded=%v, calls=%d", loaded, sender.calls)
			}
		})
	}
}

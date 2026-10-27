package agent

import (
	"context"
	"reflect"
	"testing"

	"github.com/open-octo/octo-agent/internal/hooks"
)

func TestCanonicalToolName(t *testing.T) {
	tools := []ToolDefinition{{Name: "skill"}, {Name: "glob"}, {Name: "write_file"}, {Name: "mcp__Server__Fetch"}}
	for _, tc := range []struct{ name, want string }{
		{"Skill", "skill"}, {"SKILL", "skill"}, {"skill", "skill"},
		{"Glob", "glob"}, {"Write_File", "write_file"},
		{"mcp__Server__Fetch", "mcp__Server__Fetch"},
		{"Bash", "Bash"}, {"missing", "missing"}, {" skill ", " skill "},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := canonicalToolName(tc.name, tools); got != tc.want {
				t.Fatalf("canonicalToolName(%q) = %q, want %q", tc.name, got, tc.want)
			}
		})
	}
	// Tool names outside this turn's catalog must not gain a valid identity.
	if got := canonicalToolName("Skill", []ToolDefinition{{Name: "glob"}}); got != "Skill" {
		t.Fatalf("unadvertised name changed to %q", got)
	}
	colliding := []ToolDefinition{{Name: "skill"}, {Name: "Skill"}}
	for _, name := range []string{"skill", "Skill", "SKILL"} {
		if got := canonicalToolName(name, colliding); got != name {
			t.Fatalf("exact/ambiguous name %q changed to %q", name, got)
		}
	}
}

func TestAgent_ToolNamesCanonicalBeforeGateAndExecution(t *testing.T) {
	for _, stream := range []bool{false, true} {
		for _, deny := range []string{"", "skill"} {
			t.Run(map[bool]string{false: "buffered", true: "streamed"}[stream]+"/deny="+deny, func(t *testing.T) {
				blocks := []ContentBlock{NewToolUseBlock("skill-call", "Skill", map[string]any{"name": "cron-task-creator"})}
				sender := &fakeToolSender{replies: []Reply{
					{StopReason: "tool_use", Blocks: blocks},
					{StopReason: "end_turn", Content: "done"},
				}}
				a := New(sender, "test")
				a.Gate = denyOneGate{deny: deny}
				a.Hooks = hooks.NewEngine(nil)
				a.Hooks.RegisterInProc(hooks.EventPreToolUse, func(_ context.Context, p hooks.Payload) string {
					if p.ToolName != "skill" {
						t.Errorf("PreToolUse hook saw %q, want skill", p.ToolName)
					}
					return ""
				})
				executor := &fakeExecutor{}
				defs := []ToolDefinition{{Name: "skill"}}
				var events []AgentEvent
				var err error
				if stream {
					_, err = a.RunStream(context.Background(), "/cron-task-creator", defs, executor, func(ev AgentEvent) { events = append(events, ev) })
				} else {
					_, err = a.Run(context.Background(), "/cron-task-creator", defs, executor)
				}
				if err != nil {
					t.Fatal(err)
				}
				if deny == "skill" {
					if len(executor.called) != 0 {
						t.Fatalf("case variation bypassed the gate: %v", executor.called)
					}
				} else if !reflect.DeepEqual(executor.called, []string{"skill"}) {
					t.Fatalf("executor saw %v, want [skill]", executor.called)
				}
				for _, msg := range a.History.Snapshot() {
					for _, block := range msg.Blocks {
						if block.Type == "tool_use" && (block.Name != "skill" || block.ID != "skill-call" || block.Input["name"] != "cron-task-creator") {
							t.Fatalf("history lost canonical name, ID or arguments: %+v", block)
						}
						if block.Type == "tool_result" && (block.ToolUseID != "skill-call" || block.IsError != (deny != "")) {
							t.Fatalf("incorrect tool result: %+v", block)
						}
					}
				}
				for _, ev := range events {
					if ev.ToolName != "" && ev.ToolName != "skill" {
						t.Fatalf("event used noncanonical name %q", ev.ToolName)
					}
				}
				if blocks[0].Name != "Skill" {
					t.Fatal("modified the sender's reply slice")
				}
			})
		}
	}
}

func TestAgent_StreamedToolInputUsesCanonicalName(t *testing.T) {
	sender := &fakeToolStreamingSender{
		fakeToolSender: fakeToolSender{replies: []Reply{
			{StopReason: "tool_use", Blocks: []ContentBlock{NewToolUseBlock("load", "Skill", map[string]any{"name": "cron-task-creator"})}},
			{StopReason: "end_turn", Content: "done"},
		}},
		toolDeltas: []struct{ id, name, partialJSON string }{{"load", "Skill", `{"name":"cron-task-creator"}`}},
	}
	a := New(sender, "fixture")
	var deltas int
	_, err := a.RunStream(context.Background(), "load", []ToolDefinition{{Name: "skill"}}, &fakeExecutor{}, func(ev AgentEvent) {
		if ev.Kind == EventToolInputDelta {
			deltas++
			if ev.ToolName != "skill" || ev.ToolID != "load" {
				t.Errorf("incorrect input delta identity: %+v", ev)
			}
		}
	})
	if err != nil || deltas != 1 {
		t.Fatalf("streamed input: deltas=%d err=%v", deltas, err)
	}
}

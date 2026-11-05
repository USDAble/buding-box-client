package tools

import (
	"context"
	"fmt"
	"strings"
	"testing"

	"github.com/open-octo/octo-agent/internal/agent"
)

type catalogNameExecutor struct{}

func (catalogNameExecutor) Execute(_ context.Context, name string, _ map[string]any) (agent.ToolResult, error) {
	return agent.ToolResult{Text: name}, nil
}

// Include gated tools too: checking only DefaultTools would miss newly registered capabilities.
func TestBuiltinToolCatalogNamesAndSchemas(t *testing.T) {
	owners := make(map[string]string)
	var defs []agent.ToolDefinition
	var blocks []agent.ContentBlock
	want := make(map[string]string)
	for _, tool := range allTools {
		def := tool.Definition()
		defs = append(defs, def)
		for _, name := range append([]string{def.Name}, def.Aliases...) {
			key := strings.ToLower(name)
			if strings.TrimSpace(name) != name || name == "" {
				t.Fatalf("%q has invalid tool name or alias %q", def.Name, name)
			}
			if owner, exists := owners[key]; exists {
				t.Fatalf("%q collides between %q and %q", name, owner, def.Name)
			}
			owners[key] = def.Name
			id := fmt.Sprintf("tool-%d", len(blocks))
			blocks = append(blocks, agent.NewToolUseBlock(id, strings.ToUpper(name), nil))
			want[id] = def.Name
		}
		if def.Parameters["type"] != "object" || def.Description == "" {
			t.Fatalf("%q has an invalid definition", def.Name)
		}
		props, _ := def.Parameters["properties"].(map[string]any)
		for _, key := range stringSliceArg(def.Parameters, "required") {
			if _, exists := props[key]; !exists {
				t.Fatalf("%q requires undefined parameter %q", def.Name, key)
			}
		}
	}
	if len(defs) == 0 {
		t.Fatal("no built-in tools examined")
	}
	sender := &casingSender{blocks: blocks}
	a := agent.New(sender, "fixture")
	if _, err := a.RunStream(context.Background(), "run tools", defs, catalogNameExecutor{}, nil); err != nil {
		t.Fatal(err)
	}
	seen := 0
	for _, message := range a.History.Snapshot() {
		for _, block := range message.Blocks {
			if block.Type == "tool_result" {
				seen++
				if block.IsError || block.Result != want[block.ToolUseID] {
					t.Errorf("%s: result %q, want %q", block.ToolUseID, block.Result, want[block.ToolUseID])
				}
			}
		}
	}
	if seen != len(want) {
		t.Fatalf("checked %d results, want %d", seen, len(want))
	}
	t.Logf("checked %d built-in tools and %d names/aliases", len(defs), len(want))
}

func TestAgent_BashUsesRealTerminal(t *testing.T) {
	sender := &casingSender{blocks: []agent.ContentBlock{
		agent.NewToolUseBlock("shell", "Bash", map[string]any{"command": "echo alias-ok", "description": "Check tool alias"}),
	}}
	a := agent.New(sender, "fixture")
	if _, err := a.RunStream(context.Background(), "run", DefaultTools(), NewDefaultRegistry(), nil); err != nil {
		t.Fatal(err)
	}
	for _, message := range a.History.Snapshot() {
		for _, block := range message.Blocks {
			if block.Type == "tool_result" && block.ToolUseID == "shell" {
				if block.IsError || !strings.Contains(block.Result, "alias-ok") {
					t.Fatalf("Bash did not reach terminal: %+v", block)
				}
				return
			}
		}
	}
	t.Fatal("missing terminal result")
}

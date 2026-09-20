package tools

import (
	"context"
	"fmt"
	"github.com/open-octo/octo-agent/internal/agent"
	"strings"
)

// OCTO-FORK: platform instructions belong to the authenticated pinned session,
// never a process-global registry shared by different experts or accounts.
type platformSkillLoaderKey struct{}

func WithPlatformSkillLoader(ctx context.Context, loader func(context.Context, string) (string, error)) context.Context {
	return context.WithValue(ctx, platformSkillLoaderKey{}, loader)
}
func platformSkillLoader(ctx context.Context) func(context.Context, string) (string, error) {
	loader, _ := ctx.Value(platformSkillLoaderKey{}).(func(context.Context, string) (string, error))
	return loader
}

type PlatformSkillTool struct{}

func (PlatformSkillTool) Definition() agent.ToolDefinition {
	return agent.ToolDefinition{Name: "load_platform_skill", Description: "Load the full instructions of a skill listed by the selected platform expert using its exact skill_id. Follow the returned instructions with available tools. Loading instructions does not execute or complete the task. Report loading or execution failures honestly.", Parameters: map[string]any{"type": "object", "properties": map[string]any{"skill_id": map[string]any{"type": "string", "description": "Exact skill ID from the selected expert's available skills index."}}, "required": []string{"skill_id"}, "additionalProperties": false}}
}
func (PlatformSkillTool) Execute(ctx context.Context, _ string, input map[string]any) (agent.ToolResult, error) {
	id, _ := input["skill_id"].(string)
	if strings.TrimSpace(id) == "" || len(id) > 128 {
		return agent.ToolResult{}, fmt.Errorf("load_platform_skill: a valid skill_id is required")
	}
	loader := platformSkillLoader(ctx)
	if loader == nil {
		return agent.ToolResult{}, fmt.Errorf("load_platform_skill: no platform expert is selected")
	}
	body, err := loader(ctx, id)
	if err != nil {
		return agent.ToolResult{}, fmt.Errorf("load_platform_skill: instructions were not loaded: %w", err)
	}
	if strings.TrimSpace(body) == "" {
		return agent.ToolResult{}, fmt.Errorf("load_platform_skill: published instructions are empty")
	}
	return agent.ToolResult{Text: body}, nil
}

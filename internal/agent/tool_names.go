package agent

import "strings"

// OCTO-FORK: resolve model casing against this turn's advertised tools before
// history, hooks, permissions and execution consume the call's identity.
func canonicalizeToolCallNames(blocks []ContentBlock, tools []ToolDefinition) []ContentBlock {
	out := append([]ContentBlock(nil), blocks...)
	for i := range out {
		if out[i].Type == "tool_use" {
			out[i].Name = canonicalToolName(out[i].Name, tools)
		}
	}
	return out
}

func canonicalToolName(name string, tools []ToolDefinition) string {
	for _, tool := range tools {
		if tool.Name == name {
			return name
		}
	}
	match := ""
	for _, tool := range tools {
		if strings.EqualFold(tool.Name, name) {
			if match != "" && match != tool.Name {
				return name // Ambiguous names must not select another tool.
			}
			match = tool.Name
		}
	}
	if match != "" {
		return match
	}
	return name
}

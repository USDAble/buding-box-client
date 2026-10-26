package server

import "github.com/open-octo/octo-agent/internal/agent"

// OCTO-FORK: the sender is built once per user turn. Snapshot earlier tool IDs
// here, before the agent appends this turn's calls, images or steering messages.
func historyToolCallIDs(messages []agent.Message) []string {
	ids := make([]string, 0)
	seen := make(map[string]bool)
	for _, message := range messages {
		if message.Role != agent.RoleAssistant {
			continue
		}
		for _, block := range message.Blocks {
			if block.Type == "tool_use" && block.ID != "" && !seen[block.ID] {
				seen[block.ID] = true
				ids = append(ids, block.ID)
			}
		}
	}
	return ids
}

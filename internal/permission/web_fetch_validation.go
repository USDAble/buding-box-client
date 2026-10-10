package permission

// OCTO-FORK: host parse failures cannot fall through to implicit or remembered approval.
func invalidWebFetchURL(toolName string, input map[string]any) bool {
	if toolName != "web_fetch" {
		return false
	}
	raw, _ := input["url"].(string)
	return parseHost(raw) == ""
}

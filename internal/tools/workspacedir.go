package tools

import (
	"strings"

	"github.com/open-octo/octo-agent/internal/config"
	"github.com/open-octo/octo-agent/internal/datapath"
)

// ResolveWorkspaceDir turns the config workspace_dir value into the directory
// path a newly created web session should default its WorkingDir to.
//
//   - "" (unset) and the legacy "auto" -> data/workspace, the discoverable
//     default for every user. "auto" was the pre-#1986 magic string; the
//     Windows/macOS installers seeded it into fresh configs, so it must keep
//     resolving to the default rather than becoming a literal relative path
//     named "auto".
//   - anything else -> an explicit override, with a leading "~" expanded to
//     the user's home directory.
func ResolveWorkspaceDir(raw string) (string, error) {
	if v := strings.TrimSpace(raw); v != "" && !strings.EqualFold(v, "auto") {
		return expandHome(v), nil
	}
	// OCTO-FORK: the default workspace is data/workspace, not ~/Octo — see
	// dev-docs-usdable/需求/2260906/技术方案/P1-便携数据根.md.
	return datapath.Sub("workspace")
}

// ConfiguredWorkspaceDir resolves the workspace from the config file, for
// callers with no running server to ask (the IM bridges creating a session, the
// CLI deciding whether a directory is the workspace). Returns "" when it cannot
// be resolved at all, which callers treat as "no workspace" rather than as an
// error — every one of them has a reasonable thing to do without it.
//
// One place rather than a config.Load + ResolveWorkspaceDir pair per caller: the
// two must agree on what the workspace is, or a session gets filed under a
// directory that another check does not consider the workspace.
func ConfiguredWorkspaceDir() string {
	raw := ""
	if cfg, err := config.Load(); err == nil {
		raw = cfg.WorkspaceDir
	}
	dir, err := ResolveWorkspaceDir(raw)
	if err != nil {
		return ""
	}
	return dir
}

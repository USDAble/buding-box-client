package main

import "github.com/open-octo/octo-agent/internal/server"

// startUploadsHousekeeping ages out old upload/attachment files in the
// background at startup (#2004) — see server.StartUploadsHousekeeping for
// what it sweeps. Mirrors startTrashHousekeeping's shape and reuses its
// housekeepingDisabled guard so `go test` never touches a developer's real
// OCTO-FORK: 数据根：`~/.octo` → `<exe dir>/data`（硬规则 1） — see dev-docs-usdable/需求/2260906/技术方案/P1-便携数据根.md
// data/uploads (many cmd/octo tests drive runServe).
func startUploadsHousekeeping() {
	if housekeepingDisabled {
		return
	}
	server.StartUploadsHousekeeping()
}

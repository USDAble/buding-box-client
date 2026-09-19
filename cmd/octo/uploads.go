package main

import "github.com/open-octo/octo-agent/internal/server"

// startUploadsHousekeeping ages out old upload/attachment files in the
// background at startup (#2004) — see server.StartUploadsHousekeeping for
// what it sweeps. Mirrors startTrashHousekeeping's shape and reuses its
// housekeepingDisabled guard so `go test` never touches a developer's real
// OCTO-FORK: 数据根：`~/.octo` → `<exe dir>/data`（硬规则 1） — see the portable data-root boundary
// data/uploads (many cmd/octo tests drive runServe).
func startUploadsHousekeeping() {
	if housekeepingDisabled {
		return
	}
	server.StartUploadsHousekeeping()
}

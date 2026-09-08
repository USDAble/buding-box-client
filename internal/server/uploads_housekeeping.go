package server

import (
	"github.com/open-octo/octo-agent/internal/config"
	"github.com/open-octo/octo-agent/internal/datapath"
	"github.com/open-octo/octo-agent/internal/uploads"
)

// channelTempAdapters lists the IM channel names that stage inbound file
// attachments under a namespaced OS-temp subdirectory (uploads.ChannelTempDir)
// instead of the bare OS temp root. Listed explicitly rather than derived
// reflectively, so wiring in a new adapter's cleanup is a one-line addition.
var channelTempAdapters = []string{"telegram", "dingtalk", "feishu", "discord", "wecom", "weixin"}

// StartUploadsHousekeeping ages out old attachment files in the background
// (#2004): data/uploads (web uploads + IM inline images) and each IM
// channel adapter's namespaced temp directory (IM document/file attachments).
// Best-effort and safe to call unconditionally at server startup — both
// `octo serve` (cmd/octo, gated behind its own go-test housekeepingDisabled
// guard) and the desktop hub (cmd/octo-desktop, whose startHub only runs
// inside a live GUI event loop, never from `go test`) call this once.
func StartUploadsHousekeeping() {
	cfg, err := config.Load()
	if err != nil {
		return
	}
	retention := cfg.UploadsRetention()
	if retention <= 0 {
		return
	}
	go func() {
		if dir, err := uploadsDirPath(); err == nil {
			_, _, _ = uploads.Sweep(dir, retention)
		}
		for _, adapter := range channelTempAdapters {
			if dir, err := uploads.ChannelTempDir(adapter); err == nil {
				_, _, _ = uploads.Sweep(dir, retention)
			}
		}
	}()
}

// uploadsDirPath returns data/uploads without creating it. Unlike
// ensureUploadsDir (used by callers about to write a file there), the startup
// sweep has no reason to spin the directory into existence for an install
// that has never received an upload.
// OCTO-FORK: the portable product keeps uploads next to the executable, not in
// the host home — see dev-docs-usdable/需求/2260906/技术方案/P1-便携数据根.md.
func uploadsDirPath() (string, error) {
	return datapath.Join(uploadsDirName)
}

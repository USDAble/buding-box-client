//go:build !product_production

package app

import (
	"github.com/open-octo/octo-agent/internal/datapath"
	"github.com/open-octo/octo-agent/internal/provider"
	"github.com/open-octo/octo-agent/internal/provider/local"
)

// OCTO-FORK: P11 假模型通道 — the local fake channel bypasses the vendor
// registry entirely: no base URL, no API key, no wire protocol. The log path is
// resolved here (bootstrap layer) so internal/provider/local stays free of
// datapath. See dev-docs-usdable/需求/2260906/技术方案/P11-本地确定性模型通道.md §3.
//
// This lives in its own file, and buildClient only names newLocalProvider, so
// the production build swaps in localsender_production.go without sender.go
// growing a build tag or importing datapath at all. That keeps the fork's diff
// against upstream sender.go to a single three-line hunk.
func newLocalProvider() provider.Provider {
	logPath, err := datapath.Join("logs", "local-provider.jsonl")
	if err != nil {
		logPath = ""
	}
	return local.New(logPath)
}

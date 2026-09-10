//go:build product_production

package app

import (
	"github.com/open-octo/octo-agent/internal/provider"
	"github.com/open-octo/octo-agent/internal/provider/local"
)

// OCTO-FORK: P11 — production counterpart of localsender.go.
//
// The reply engine and the data/logs/local-provider.jsonl writer it feeds are
// excluded from production builds (internal/provider/local is tagged
// !product_production), so there is no log path to resolve and nothing to
// configure. Returning the stub keeps the provider name resolvable while
// making every Send/SendStream call fail hard.
//
// Note the deliberate absence of datapath here: the reference path of the PII
// observability log must not appear in the shipped binary at all.
func newLocalProvider() provider.Provider {
	return local.New("")
}

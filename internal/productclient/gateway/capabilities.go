package gateway

import (
	"fmt"
	"sort"
	"strings"

	"github.com/open-octo/octo-agent/internal/agent"
)

// This file exists because of one specific way the observer can go wrong
// silently.
//
// The agent loop never asks a sender what it can do. It probes, at five
// independent points, with type assertions:
//
//	agent.go:770   StreamingSender
//	agent.go:848   ToolSender
//	agent.go:948   ToolStreamingSender
//	agent.go:1617  NoReasoningSender, else LowEffortSender
//
// The interfaces are not a chain: StreamingSender and ToolSender are
// incomparable, and NoReasoningSender / LowEffortSender are separate again. So a
// decorator that returns its result as a plain agent.Sender does not fail to
// compile and does not log anything — it just quietly loses streaming, or tools,
// or title generation, depending on which assertion the caller reaches first.
//
// CapabilitiesOf and PreservesCapabilities make that loss loud. The real
// observer must call PreservesCapabilities on every wrap, and the contract test
// in gateway_test.go pins it.

// capabilityNames are the interfaces the agent loop probes for, in the order the
// loop tends to reach them. The set is derived from agent.go, not from taste:
// adding a probe there means adding a name here.
var capabilityNames = []string{
	"StreamingSender",
	"ToolSender",
	"ToolStreamingSender",
	"NoReasoningSender",
	"LowEffortSender",
}

// CapabilitiesOf reports which sender capability interfaces v satisfies, as a
// sorted list of names. An empty result means "a plain Sender and nothing more".
//
// The result is a set, not a rank: no capability is a superset of another here,
// so a single "level" would be wrong.
func CapabilitiesOf(v agent.Sender) []string {
	if v == nil {
		return nil
	}
	var out []string
	if _, ok := v.(agent.StreamingSender); ok {
		out = append(out, "StreamingSender")
	}
	if _, ok := v.(agent.ToolSender); ok {
		out = append(out, "ToolSender")
	}
	if _, ok := v.(agent.ToolStreamingSender); ok {
		out = append(out, "ToolStreamingSender")
	}
	if _, ok := v.(agent.NoReasoningSender); ok {
		out = append(out, "NoReasoningSender")
	}
	if _, ok := v.(agent.LowEffortSender); ok {
		out = append(out, "LowEffortSender")
	}
	sort.Strings(out)
	return out
}

// CapabilityLoss reports which capabilities a wrapper dropped.
type CapabilityLoss struct {
	Dropped []string
}

func (e *CapabilityLoss) Error() string {
	return fmt.Sprintf("gateway: the wrapper dropped sender capabilities %s; the agent loop "+
		"probes with type assertions and will silently fall back to weaker behaviour",
		strings.Join(e.Dropped, ", "))
}

// PreservesCapabilities reports whether wrapped still offers everything base did.
//
// It checks for a *superset*, not equality: a wrapper that also implements a
// capability its base lacked is odd but safe, since it must then implement the
// method. What is never safe is dropping one.
func PreservesCapabilities(base, wrapped agent.Sender) error {
	have := map[string]bool{}
	for _, name := range CapabilitiesOf(wrapped) {
		have[name] = true
	}
	var dropped []string
	for _, name := range CapabilitiesOf(base) {
		if !have[name] {
			dropped = append(dropped, name)
		}
	}
	if len(dropped) > 0 {
		return &CapabilityLoss{Dropped: dropped}
	}
	return nil
}

// KnownCapabilityNames returns the interface names this package tracks, so a
// test can assert that the probe list here still matches agent.go.
func KnownCapabilityNames() []string {
	return append([]string(nil), capabilityNames...)
}

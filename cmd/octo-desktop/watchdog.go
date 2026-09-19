// Watchdog for the portable data root: freezes the product when the data
// directory disappears (a removable disk pulled out, a folder renamed) and
// thaws it when that same path is usable again.
//
// Detection only. The refusal lives in internal/datapath's freeze gate, so a
// frozen product refuses at the filesystem boundary rather than at the UI —
// see datapath.Freeze. This file decides *when* to flip that bit; it must never
// grow a second one, or the product could be showing a frozen screen while its
// writers keep running.
//
// See the desktop lifecycle design 的 L-E3（PR-7）与
// P2-启动与生命周期.md §3.4.
package main

import (
	"os"
	"time"

	"github.com/open-octo/octo-agent/internal/datapath"
)

// watchdogInterval is how often the watchdog re-checks the data root. A stat is
// cheap, and a 2s cadence keeps a just-pulled disk from leaving the product
// thinking it can still write for more than a moment.
const watchdogInterval = 2 * time.Second

// watchdogFailuresBeforeFreezing is the hysteresis: one failed stat is not
// enough to freeze. A blocking filesystem, a network mount that hiccups, or a
// scan by the OS can all make a single stat fail on a disk that is still there
// — and the cost of a false positive is a modal interruption plus a product
// that refuses to work until it recovers. Two consecutive failures at
// watchdogInterval mean the root has been gone for about 2s.
const watchdogFailuresBeforeFreezing = 2

// Watchdog watches the data root and flips the process-wide freeze gate.
type Watchdog struct {
	root     string
	interval time.Duration
	failures int
	onLost   func()
	onBack   func()

	stop chan struct{}
	done chan struct{}
}

// StartWatchdog begins watching root and returns the running Watchdog. It calls
// onLost once when the root becomes unavailable and onBack once when the same
// path is usable again. Both run on the watchdog's own goroutine.
func StartWatchdog(root string, onLost, onBack func()) *Watchdog {
	return startWatchdog(root, watchdogInterval, watchdogFailuresBeforeFreezing, onLost, onBack)
}

func startWatchdog(root string, interval time.Duration, failures int, onLost, onBack func()) *Watchdog {
	w := &Watchdog{
		root:     root,
		interval: interval,
		failures: failures,
		onLost:   onLost,
		onBack:   onBack,
		stop:     make(chan struct{}),
		done:     make(chan struct{}),
	}
	go w.run()
	return w
}

// Frozen reports whether writes are currently refused. It reads the datapath
// gate rather than a bit of its own, so "the watchdog thinks we are frozen" and
// "the writers refuse" cannot disagree.
func (w *Watchdog) Frozen() bool { return datapath.Frozen() }

// Stop terminates the watch loop and waits for it to exit.
func (w *Watchdog) Stop() {
	close(w.stop)
	<-w.done
}

func (w *Watchdog) run() {
	defer close(w.done)
	ticker := time.NewTicker(w.interval)
	defer ticker.Stop()

	consecutiveFailures := 0
	for {
		select {
		case <-w.stop:
			return
		case <-ticker.C:
			if w.statOK() {
				consecutiveFailures = 0
				// Only a transition thaws, so the callback fires once per loss
				// and not on every tick that finds a healthy root.
				if datapath.Frozen() {
					datapath.Thaw()
					if w.onBack != nil {
						w.onBack()
					}
				}
				continue
			}
			consecutiveFailures++
			// The gate is also the debounce: once frozen, later failures take
			// this branch no further, so a disk left out overnight produces one
			// notice rather than one every 2s (§4.2: 持续丢失不重复广播).
			if consecutiveFailures >= w.failures && !datapath.Frozen() {
				datapath.Freeze()
				if w.onLost != nil {
					w.onLost()
				}
			}
		}
	}
}

// statOK reports whether the data root is a directory we could write into.
//
// Existence alone is the test on purpose. The archive's version of this file
// also required that the directory be the same filesystem object as before the
// loss (it read a pid back out of an instance.json the product had written);
// that check is not available here and would not work if it were. A removable
// disk re-mounted is a *new* mount with a new device id, and on Unix a new
// directory inode as well — so an identity comparison rejects exactly the
// recovery this product is built around ("same path, disk plugged back in").
// The requirement it was reaching for is already structural: the watchdog
// watches one fixed path for the life of the process, so a disk that comes back
// under a different drive letter never satisfies this test and never thaws
// (§4.2: 新路径不算恢复).
//
// The gap that leaves is a different disk mounted at the same path, which this
// cannot tell apart from the original. Closing it needs a product-owned marker
// written into the data root at startup and read back on thaw — that is the
// instance/ownership record, which L-E3 does not introduce (registered as V-88
// rather than approximated here).
func (w *Watchdog) statOK() bool {
	info, err := os.Stat(w.root)
	if err != nil {
		return false
	}
	return info.IsDir()
}

// Watchdog for the portable data root: freezes the product when the data
// directory disappears (a U盘 pulled out, a folder renamed) and thaws only
// when the same path returns. See P2-启动与生命周期.md §3.4.
package main

import (
	"os"
	"time"

	"github.com/open-octo/octo-agent/internal/datapath"
)

// watchdogInterval is how often the watchdog re-checks the data root. A stat
// is cheap, and a 2s cadence keeps a just-pulled drive from leaving the UI
// thinking it can still write for more than a moment. P2 §9 leaves the door
// open to relaxing this to 5s if power/sleep profiling ever demands it.
const watchdogInterval = 2 * time.Second

// Watchdog watches the data root and flips between frozen and thawed. The
// frozen flag is the product's "do not write" gate: while set, write paths
// must refuse rather than attempt a write against a vanished directory.
type Watchdog struct {
	root       string
	interval   time.Duration
	ownPID     int
	onLost     func()
	onRestored func()

	stop chan struct{}
	done chan struct{}
}

// StartWatchdog begins watching root and returns the running Watchdog. It
// calls onLost once when the root becomes unavailable (two consecutive failed
// stats — a transient blip must not freeze the UI) and onRestored once when
// the same path returns with this process still recorded as its owner.
func StartWatchdog(root string, onLost, onRestored func()) *Watchdog {
	return startWatchdog(root, watchdogInterval, os.Getpid(), onLost, onRestored)
}

func startWatchdog(root string, interval time.Duration, ownPID int, onLost, onRestored func()) *Watchdog {
	w := &Watchdog{
		root:       root,
		interval:   interval,
		ownPID:     ownPID,
		onLost:     onLost,
		onRestored: onRestored,
		stop:       make(chan struct{}),
		done:       make(chan struct{}),
	}
	go w.run()
	return w
}

// Frozen reports whether the data root is currently considered unavailable.
// It reflects the process-wide datapath freeze gate (datapath.Freeze/Thaw),
// which the directory-creating entry points check before any write — so a
// frozen watchdog and a refusing write path are the same bit, not two.
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
				if datapath.Frozen() && w.isOwnInstance() {
					datapath.Thaw()
					if w.onRestored != nil {
						w.onRestored()
					}
				}
				continue
			}
			consecutiveFailures++
			if consecutiveFailures >= 2 && !datapath.Frozen() {
				datapath.Freeze()
				if w.onLost != nil {
					w.onLost()
				}
			}
		}
	}
}

// statOK reports whether the data root directory exists.
func (w *Watchdog) statOK() bool {
	info, err := os.Stat(w.root)
	return err == nil && info.IsDir()
}

// isOwnInstance confirms this process still owns the data root: the thaw only
// happens when the instance.json written at startup still records our pid. A
// vanished-and-returned drive re-plugged into a different slot (a different
// path) never reaches here — statOK stays false — but if it did, the pid check
// is the second line of defense against thawing someone else's directory.
func (w *Watchdog) isOwnInstance() bool {
	inst, ok := readInstanceJSON()
	return ok && inst.PID == w.ownPID
}

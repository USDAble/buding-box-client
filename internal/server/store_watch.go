package server

import (
	"context"
	"log/slog"
	"os"
	"strings"
	"time"

	"github.com/open-octo/octo-agent/internal/agent"
)

// The sidebar learns that a session appeared, or that the project registry
// changed, from broadcasts this process emits as it makes those changes. That
// covers nothing another process does — and another process routinely does:
// `octo` in a terminal creates sessions and files them under projects
// OCTO-FORK: 数据根：`~/.octo` → `<exe dir>/data`（硬规则 1） — see dev-docs-usdable/需求/2260906/技术方案/P1-便携数据根.md
// (EnsureProjectForDir) against the same data root, and a second `octo serve` or
// the desktop shell can too.
//
// Nothing was ever wrong on screen, since every read goes to disk: listSessions
// re-reads the directory and cachedRegistry re-stats the registry, so a reload
// shows the truth. What was missing is being told, so an open sidebar sat on a
// stale snapshot until something else happened to refresh it.
//
// This polls for that. There is no fsnotify anywhere in the tree (see
// agentprofile's note) and adding a watcher dependency to notice a file change
// every few seconds is not a trade worth making.

// storeWatchInterval is how often the store is sampled. The delay it costs is
// how long an open sidebar can lag a change made elsewhere; a few seconds is
// imperceptible for "a session appeared in another window", and the sampling
// is two syscalls.
const storeWatchInterval = 5 * time.Second

// storeFingerprint is what the watcher compares between samples.
//
// What must NOT be in here is anything that changes when a transcript's
// CONTENTS change. Every broadcast makes every open tab re-list sessions, which
// loads every transcript, so a signal that moved on each turn would be a
// standing cost for the length of every conversation. Writes stay invisible
// here: Session.Save appends, and its rewrite path truncates the file in place
// (O_TRUNC) rather than renaming a temp file over it, so neither touches the
// directory or the file count.
type storeFingerprint struct {
	// sessionCount is how many transcripts are on disk, and sessionsModTime is
	// the mtime of the directory holding them. Both, because each covers the
	// other's blind spot:
	//
	//   - The count misses an add and a remove landing in the same interval,
	//     which the directory's mtime catches.
	//   - The mtime misses two sessions created within one filesystem mtime
	//     tick, since the first already moved it; the count catches that.
	//
	// The mtime also moves for a `<id>.chunks` archive directory appearing
	// alongside the transcripts (Session.ChunkDir), which is not a new session.
	// That costs one redundant refetch, the cheap direction to be wrong in: a
	// spurious announcement is idempotent, a missed one leaves the sidebar a row
	// short until something else changes.
	sessionCount    int
	sessionsModTime time.Time
	// registryModTime and registrySize identify the project registry, the same
	// pair cachedRegistry invalidates on.
	registryModTime time.Time
	registrySize    int64
}

// sampleStore reads the current fingerprint. Errors resolve to a zero value
// rather than propagating: a missing sessions directory or registry is the
// normal state of a fresh install, and an unreadable one is not something a
// watcher should escalate.
func sampleStore() storeFingerprint {
	var fp storeFingerprint
	// SessionsDirPath, not SessionsDir: this is a read, and the creating form
	// resolves through datapath.Root, which makes the directory and writes a
	// probe file into it — five seconds apart, for as long as the server lives.
	// The watcher must not be the thing that materialises the store it watches
	// (V-105).
	if dir, err := agent.SessionsDirPath(); err == nil {
		if info, err := os.Stat(dir); err == nil {
			fp.sessionsModTime = info.ModTime()
		}
		if entries, err := os.ReadDir(dir); err == nil {
			for _, e := range entries {
				if !e.IsDir() && strings.HasSuffix(e.Name(), ".jsonl") {
					fp.sessionCount++
				}
			}
		}
	}
	if path, err := sessionGroupsPath(); err == nil {
		if info, err := os.Stat(path); err == nil {
			fp.registryModTime = info.ModTime()
			fp.registrySize = info.Size()
		}
	}
	return fp
}

// startStoreWatch begins sampling the data root for changes made outside this
// process, broadcasting the events an open sidebar already knows how to act
// on. Started by serveOn; stopped by doShutdown (which closes watchStop),
// then joined by joinStoreWatch.
//
// The join is the half that was missing (V-105): a channel close is not a join,
// so a sample already running when the close landed still resolved the data
// root — and did it into whatever root was current by the time it got there,
// which for a test harness is the next test's.
//
// The first sample is a baseline and announces nothing — otherwise every start
// would tell every tab to refetch for no reason.
func (s *Server) startStoreWatch() {
	s.watchStarted.Store(true)
	go func() {
		defer s.recoverBg("store watch")
		defer close(s.watchDone)
		prev := s.sample()
		t := time.NewTicker(storeWatchInterval)
		defer t.Stop()
		for {
			select {
			case <-s.watchStop:
				return
			case <-t.C:
				prev = s.pollStoreOnce(prev)
			}
		}
	}()
}

// joinStoreWatch waits for the watcher goroutine to return, after doShutdown
// closed watchStop.
//
// A close only asks. The join is what makes "shut down" mean "and it will not
// touch the data root again": a sample already running when the close landed
// still resolves data/sessions and the registry file, so a caller that releases
// the root (a test's TempDir, an unmounted drive) would otherwise lose a race
// with a write nobody is waiting for (V-105).
//
// Skipped when the watch never ran: a server shut down without serving has
// nothing to join, and waiting would burn the whole ctx.
// OCTO-FORK: the join Scheduler/store-watch shutdown never had — see dev-docs-usdable/需求/20260911/需求基线.md §5.6.
func (s *Server) joinStoreWatch(ctx context.Context) {
	if !s.watchStarted.Load() {
		return
	}
	select {
	case <-s.watchDone:
	case <-ctx.Done():
	}
}

// sample reads the current fingerprint. Split out from sampleStore so the V-105
// nail can hold a sample open across Shutdown and see whether Shutdown waited
// for it — an ordering a test cannot observe any other way. The barrier is nil
// in production.
func (s *Server) sample() storeFingerprint {
	if s.storeSampleBarrier != nil {
		s.storeSampleBarrier()
	}
	return sampleStore()
}

// pollStoreOnce samples once, broadcasts what changed, and returns the new
// fingerprint. Split out so a test can drive iterations without waiting on the
// ticker.
//
// A change this server made itself is indistinguishable from another
// process's and gets announced too. That costs one extra refetch, which is
// harmless — both events make the frontend refetch wholesale, so it is
// idempotent — and the alternative (bookkeeping every local write so the
// watcher can recognise its own) is a large amount of easily-stale machinery
// for a cost that is already bounded by the sampling interval.
func (s *Server) pollStoreOnce(prev storeFingerprint) storeFingerprint {
	cur := s.sample()
	if cur.sessionCount != prev.sessionCount || !cur.sessionsModTime.Equal(prev.sessionsModTime) {
		slog.Debug("store watch: transcripts changed",
			"count", cur.sessionCount, "was", prev.sessionCount)
		// session_created is what the frontend already handles by refetching
		// the session list AND the groups snapshot, which is what is needed
		// whichever way the store moved — including a removal, since the
		// refetch is wholesale.
		s.wsHub.broadcast("", wsEventSessionCreated{Type: "session_created"})
	}
	if !cur.registryModTime.Equal(prev.registryModTime) || cur.registrySize != prev.registrySize {
		slog.Debug("store watch: project registry changed")
		s.wsHub.broadcast("", wsEventSessionGroupsChanged{Type: "session_groups_changed"})
	}
	return cur
}

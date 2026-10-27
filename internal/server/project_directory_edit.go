package server

import (
	"fmt"
	"os"
	"sync"

	"github.com/open-octo/octo-agent/internal/agent"
)

// OCTO-FORK: the server owns the pre-message directory lock for every client.
type sessionGroupResponse struct {
	sessionGroup
	WorkingDirLocked bool `json:"working_dir_locked"`
}

func (s *Server) projectGroupResponse(g sessionGroup) sessionGroupResponse {
	unlock, err := s.lockProjectDirectory(g)
	unlock()
	return sessionGroupResponse{sessionGroup: g, WorkingDirLocked: err != nil}
}

// The caller holds groupMu so membership cannot change during the edit.
// TryLock avoids reversing the turn -> registry lock order used by sends.
func (s *Server) lockProjectDirectory(g sessionGroup) (func(), error) {
	var held []*sync.Mutex
	unlock := func() {
		for _, mu := range held {
			mu.Unlock()
		}
		held = nil
	}
	for _, id := range g.SessionIDs {
		mu := s.sessionTurnLock(id)
		if !mu.TryLock() {
			unlock()
			return unlock, fmt.Errorf("project working directory cannot change while a session is busy")
		}
		held = append(held, mu)
		_, err := agent.SessionMTime(id)
		if os.IsNotExist(err) {
			continue
		}
		sess, err := agent.LoadSession(id)
		if err != nil {
			unlock()
			return unlock, fmt.Errorf("check project session: %w", err)
		}
		_, leased := sess.LeaseActive()
		if s.turnRunning[id] || leased || sess.ProtectionPolicy.Locked || len(sess.Messages) > 0 {
			unlock()
			return unlock, fmt.Errorf("project working directory cannot change after the first message")
		}
	}
	return unlock, nil
}

package server

// OCTO-FORK: reuse the session owner to cancel obsolete optional model work.
import "context"

type suggestionTurn struct{ cancel context.CancelFunc }

func (s *Server) beginSuggestionTurn(sessionID string) *suggestionTurn {
	s.suggestionMu.Lock()
	defer s.suggestionMu.Unlock()
	if old := s.suggestions[sessionID]; old != nil && old.cancel != nil {
		old.cancel()
	}
	if s.suggestions == nil {
		s.suggestions = make(map[string]*suggestionTurn)
	}
	turn := &suggestionTurn{}
	s.suggestions[sessionID] = turn
	return turn
}

func (s *Server) claimSuggestion(sessionID string, turn *suggestionTurn) (context.Context, context.CancelFunc, bool) {
	s.suggestionMu.Lock()
	defer s.suggestionMu.Unlock()
	if s.suggestions[sessionID] != turn {
		return nil, nil, false
	}
	ctx, cancel := context.WithTimeout(context.Background(), throwawayGenerationTimeout)
	turn.cancel = cancel
	return ctx, cancel, true
}

// The caller releases immediately after enqueueing the event, so a newer turn's
// start cannot overtake an already-approved publication on the same event queue.
func (s *Server) claimSuggestionPublication(sessionID string, turn *suggestionTurn, ctx context.Context) func() {
	s.suggestionMu.Lock()
	if s.suggestions[sessionID] != turn {
		s.suggestionMu.Unlock()
		return nil
	}
	delete(s.suggestions, sessionID)
	if ctx.Err() == context.Canceled {
		s.suggestionMu.Unlock()
		return nil
	}
	return s.suggestionMu.Unlock
}

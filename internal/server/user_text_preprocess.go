// OCTO-FORK: server-authoritative user-text preprocessing shared by every
// product text entry point. The server owns ordering; rule semantics stay in
// internal/pii and product preferences stay behind the injected sensitive gate.
package server

import (
	"errors"
	"fmt"
	"net/http"

	"github.com/open-octo/octo-agent/internal/agent"
	"github.com/open-octo/octo-agent/internal/pii"
)

const codePrivacyTransformFailed = "privacy_transform_failed"

// PersonalInfoTransform is the narrow construction seam shared by the desktop
// product route and the server turn path.
type PersonalInfoTransform func(text string) (pii.Result, error)

type preparedUserText struct {
	text    string
	matches []pii.MatchSummary
}

func (p preparedUserText) applied() bool { return len(p.matches) > 0 }

func mergePreparedUserText(previous, current preparedUserText) preparedUserText {
	current.matches = append(previous.matches, current.matches...)
	return current
}

type userTextError struct {
	code        string
	replacement string
	cause       error
}

func (e *userTextError) Error() string {
	if e == nil || e.cause == nil {
		return ""
	}
	return e.cause.Error()
}

func (e *userTextError) Unwrap() error {
	if e == nil {
		return nil
	}
	return e.cause
}

func personalInfoTransformOr(injected PersonalInfoTransform) PersonalInfoTransform {
	if injected != nil {
		return injected
	}
	return pii.New().Transform
}

// checkUserTextSafety is phase one of the pipeline. It runs before binding,
// interruption, history truncation, or any other side effect, preserving the
// existing content-safety refusal contract.
func (s *Server) checkUserTextSafety(text string) *userTextError {
	if masked, refuse := s.sensInputVerdict(text); refuse {
		return &userTextError{
			code:        codeInputSensitive,
			replacement: masked,
			cause:       errors.New("input contains blocked content"),
		}
	}
	return nil
}

// protectUserText is phase two of the pipeline. Callers that can race a
// pristine session's policy update invoke it only after taking the session turn
// lock and reloading the authoritative session.
func (s *Server) protectUserText(sess *agent.Session, text string) (preparedUserText, *userTextError) {
	prepared := preparedUserText{text: text}
	if sess == nil || !sess.ProtectionPolicy.PersonalInfoProtection {
		return prepared, nil
	}
	transform := s.personalInfoTransform
	if transform == nil {
		// Minimal upstream tests construct Server values directly rather than
		// through New. The built-in engine has no external dependency, so nil
		// still gets the safe production default; injected errors remain the
		// fail-closed path exercised by protection tests.
		transform = pii.New().Transform
	}
	result, err := transform(text)
	if err != nil {
		return preparedUserText{}, privacyTransformError(err)
	}
	prepared.text = result.Masked
	prepared.matches = append(prepared.matches, result.Matches...)
	return prepared, nil
}

func privacyTransformError(err error) *userTextError {
	return &userTextError{
		code:  codePrivacyTransformFailed,
		cause: fmt.Errorf("personal information protection failed: %w", err),
	}
}

func refuseUserTextHTTP(w http.ResponseWriter, err *userTextError) {
	if err == nil {
		return
	}
	if err.code == codeInputSensitive {
		refuseSensitiveInput(w, err.replacement)
		return
	}
	writeJSON(w, http.StatusInternalServerError, map[string]string{
		"code":  err.code,
		"error": "personal information protection failed; please retry",
	})
}

func (s *Server) refuseUserTextWS(sessionID string, err *userTextError) {
	if err == nil {
		return
	}
	if err.code == codeInputSensitive {
		s.broadcastInputSensitive(sessionID, err.replacement)
		return
	}
	if s.wsHub == nil {
		return
	}
	s.wsHub.broadcast(sessionID, map[string]any{
		"type":       "send_rejected",
		"session_id": sessionID,
		"code":       err.code,
		"message":    "Personal information protection failed; please retry.",
	})
}

// broadcastPrivacyApplied emits aggregate metadata only. It is intentionally
// not part of live-state replay: reopened sessions read the already-masked
// history and do not replay a transient notification.
func (s *Server) broadcastPrivacyApplied(sessionID string, prepared preparedUserText) {
	if !prepared.applied() || s.wsHub == nil {
		return
	}
	categories := make([]string, 0, len(prepared.matches))
	total := 0
	for _, match := range prepared.matches {
		categories = append(categories, match.Category)
		total += match.Count
	}
	s.wsHub.broadcast(sessionID, map[string]any{
		"type":         "privacy_applied",
		"session_id":   sessionID,
		"categories":   categories,
		"count":        total,
		"rule_version": pii.RuleVersion,
	})
}

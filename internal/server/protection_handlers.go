package server

import (
	"errors"
	"fmt"
	"net/http"
	"strings"

	"github.com/open-octo/octo-agent/internal/agent"
	"github.com/open-octo/octo-agent/internal/config"
)

const (
	codeSessionPolicyLocked          = "session_policy_locked"
	codeConfidentialModelRequired    = "confidential_model_required"
	codeConfidentialModelUnavailable = "confidential_model_unavailable"
)

func writeProtectionError(w http.ResponseWriter, code, message string) {
	writeJSON(w, http.StatusConflict, map[string]string{"code": code, "error": message})
}

type updateProtectionRequest struct {
	PersonalInfoProtection *bool   `json:"personal_info_protection"`
	ConfidentialSession    *bool   `json:"confidential_session"`
	ModelID                *string `json:"model_id,omitempty"`
}

// handleUpdateSessionProtection owns the only pre-turn mutation that may
// change both privacy policy and model binding. It shares the turn lock with
// message execution and persists both values as one session record.
func (s *Server) handleUpdateSessionProtection(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	if id == "" {
		writeError(w, http.StatusBadRequest, "missing session id")
		return
	}
	var req updateProtectionRequest
	if err := readBodyJSON(r, &req); err != nil {
		writeInvalidJSONBody(w, err)
		return
	}
	if req.PersonalInfoProtection == nil || req.ConfidentialSession == nil {
		writeError(w, http.StatusBadRequest, "personal_info_protection and confidential_session are required")
		return
	}

	if ok, _, err := s.acquireSessionBinding(id, agent.EntryWeb, false); !ok {
		writeError(w, http.StatusConflict, err.Error())
		return
	}
	defer s.releaseSessionBinding(id, agent.EntryWeb)

	turnMu := s.sessionTurnLock(id)
	turnMu.Lock()
	defer turnMu.Unlock()
	if s.turnRunning[id] {
		// OCTO-FORK: an in-flight WebSocket turn has already crossed the
		// acceptance boundary. Treat the policy as locked even if its goroutine
		// has not flushed the lock record yet, so concurrent windows receive the
		// same stable contract as a just-completed first turn.
		writeProtectionError(w, codeSessionPolicyLocked, agent.ErrProtectionPolicyLocked.Error())
		return
	}

	sess, err := agent.LoadSession(id)
	if err != nil {
		writeError(w, http.StatusNotFound, err.Error())
		return
	}
	if sess.ProtectionPolicy.Locked {
		writeProtectionError(w, codeSessionPolicyLocked, agent.ErrProtectionPolicyLocked.Error())
		return
	}

	modelConfig, model := sess.ModelConfig, sess.Model
	updateModel := req.ModelID != nil
	eligibilityID := modelConfig
	if updateModel {
		modelConfig, model, err = s.resolveSessionModel(strings.TrimSpace(*req.ModelID))
		if err != nil {
			writeError(w, http.StatusBadRequest, err.Error())
			return
		}
		eligibilityID = modelConfig
	}
	if eligibilityID == "" {
		eligibilityID = model
	}
	if *req.ConfidentialSession && !s.confidentialModelEligible(eligibilityID) {
		writeProtectionError(w, codeConfidentialModelRequired, "confidential session requires an eligible confidential model")
		return
	}

	policy := agent.ProtectionPolicy{
		Version:                agent.ProtectionPolicyVersion,
		PersonalInfoProtection: *req.PersonalInfoProtection,
		ConfidentialSession:    *req.ConfidentialSession,
		Locked:                 false,
	}
	if err := sess.SetProtectionPolicyAndModel(policy, modelConfig, model, updateModel); err != nil {
		if errors.Is(err, agent.ErrProtectionPolicyLocked) {
			writeProtectionError(w, codeSessionPolicyLocked, err.Error())
			return
		}
		writeError(w, http.StatusInternalServerError, fmt.Sprintf("save session: %v", err))
		return
	}
	s.wsHub.broadcast(id, map[string]any{
		"type":              "session_update",
		"session_id":        id,
		"model":             sess.Model,
		"model_id":          sess.ModelConfig,
		"protection_policy": sess.ProtectionPolicy,
	})

	source := sess.Source
	if source == "" {
		source = "manual"
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"ok":                true,
		"protection_policy": sess.ProtectionPolicy,
		"model":             sess.Model,
		"model_id":          sess.ModelConfig,
		"session":           s.toSessionItem(sess, source, sess.EffectiveAgentID()),
	})
}

func (s *Server) resolveSessionModel(modelID string) (modelConfig, model string, err error) {
	if modelID == "" {
		return "", "", fmt.Errorf("model_id is required")
	}
	// OCTO-FORK: product profiles treat the picker/catalog id as an opaque
	// gateway identity and never consult config.yml while resolving it.
	if !s.cfg.RequireGateway {
		cfg, _ := config.Load()
		if entry, ok := cfg.EntryByModel(modelID); ok {
			return modelID, entry.Model, nil
		}
		if modelID == "default" {
			entry := cfg.DefaultEntry()
			if entry.Model == "" {
				return "", "", fmt.Errorf("no default model configured")
			}
			return "", entry.Model, nil
		}
	} else if modelID == "default" {
		return "", "", fmt.Errorf("product profile requires a gateway catalog model")
	}
	return "", modelID, nil
}

func (s *Server) confidentialModelEligible(modelID string) bool {
	if s.cfg.ConfidentialModel != nil {
		catalogID := strings.TrimPrefix(modelID, s.cfg.GatewayModelPrefix)
		if eligible, known := s.cfg.ConfidentialModel(catalogID); known {
			return eligible
		}
	}
	if s.cfg.RequireGateway {
		return false
	}
	cfg, _ := config.Load()
	if modelID == "" || modelID == "default" {
		return cfg.DefaultEntry().Confidential
	}
	entry, ok := cfg.EntryByModel(modelID)
	return ok && entry.Confidential
}

func (s *Server) validateConfidentialSession(sess *agent.Session) error {
	if sess == nil || !sess.ProtectionPolicy.ConfidentialSession {
		return nil
	}
	modelID := sess.ModelConfig
	if modelID == "" {
		modelID = sess.Model
	}
	if !s.confidentialModelEligible(modelID) {
		return codedRefusal{
			code: codeConfidentialModelUnavailable,
			msg:  fmt.Sprintf("confidential model %q is no longer eligible; the turn was not started and nothing was sent", modelID),
		}
	}
	return nil
}

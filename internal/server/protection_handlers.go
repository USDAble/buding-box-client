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
		var ok bool
		modelConfig, model, ok = s.preferredConfidentialModel()
		if !ok {
			writeProtectionError(w, codeConfidentialModelRequired, "confidential session requires an eligible confidential model")
			return
		}
		updateModel = true
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
	} else {
		if modelID == "default" {
			return "", "", fmt.Errorf("product profile requires a gateway catalog model")
		}
		// OCTO-FORK: an empty prefix matches every string; product mode must
		// fail closed unless the desktop shell supplied the gateway namespace.
		if s.cfg.GatewayModelPrefix == "" || !strings.HasPrefix(modelID, s.cfg.GatewayModelPrefix) {
			return "", "", fmt.Errorf("product profile requires a gateway catalog model")
		}
		return modelID, strings.TrimPrefix(modelID, s.cfg.GatewayModelPrefix), nil
	}
	return "", modelID, nil
}

func (s *Server) confidentialModelEligible(modelID string) bool {
	// OCTO-FORK: a developer-local endpoint is not a missing central catalog
	// row. Keep the two identities separate so a usable signed catalog cannot
	// revoke the local private-model test path.
	isGatewayModel := s.cfg.GatewayModelPrefix != "" && strings.HasPrefix(modelID, s.cfg.GatewayModelPrefix)
	if !s.cfg.RequireGateway && !isGatewayModel {
		cfg, _ := config.Load()
		if modelID == "" || modelID == "default" {
			return cfg.DefaultEntry().Confidential
		}
		if entry, ok := cfg.EntryByModel(modelID); ok {
			return entry.Confidential
		}
	}
	if s.cfg.CatalogModel != nil {
		catalogID := strings.TrimPrefix(modelID, s.cfg.GatewayModelPrefix)
		if status, known := s.cfg.CatalogModel(catalogID); known {
			return status.Current && status.Selectable && status.Confidential
		}
	}
	return false
}

func (s *Server) preferredConfidentialModel() (modelConfig, model string, ok bool) {
	if s.cfg.PreferredConfidentialModel != nil {
		if id, found := s.cfg.PreferredConfidentialModel(); found {
			modelConfig, model, err := s.resolveSessionModel(id)
			if err == nil && s.confidentialModelEligible(protectionModelID(modelConfig, model)) {
				return modelConfig, model, true
			}
		}
	}
	if s.cfg.RequireGateway {
		return "", "", false
	}
	cfg, _ := config.Load()
	if entry := cfg.DefaultEntry(); entry.Model != "" && entry.Confidential {
		if cfg.Default != "" {
			return cfg.Default, entry.Model, true
		}
	}
	for _, endpoint := range cfg.Endpoints {
		for _, candidate := range endpoint.Models {
			if candidate.Confidential {
				return endpoint.ID + "::" + candidate.Model, candidate.Model, true
			}
		}
	}
	return "", "", false
}

func protectionModelID(values ...string) string {
	for _, value := range values {
		if value != "" {
			return value
		}
	}
	return ""
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

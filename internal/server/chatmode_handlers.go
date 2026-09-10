package server

import (
	"net/http"

	"github.com/open-octo/octo-agent/internal/agent"
	"github.com/open-octo/octo-agent/internal/chatmode"
	"github.com/open-octo/octo-agent/internal/config"
	"github.com/open-octo/octo-agent/internal/datapath"
)

// Chat-mode handlers (P9): the selector's two-level mode→model grouping and
// the per-session mode attribute. OCTO-FORK: P9 模式与模型选择器 — see
// dev-docs-usdable/需求/2260906/技术方案/P9-模式与模型.md.

// chatModeModelJSON is one model under a mode. CompositeID is the
// "<endpoint>::<model>" id the existing PATCH /sessions/{id}/model handler
// accepts; empty means the model is not resolvable in config.yml's endpoint
// system (so it cannot be selected yet).
//
// OCTO-FORK: P9 — the factory grouping carries no model ids at all: the modes
// are product-fixed, the models inside them are a projection of the signed
// central-platform catalog (P0-04). Until that lands, a stock install serves
// the three empty groups from chatmode.Builtin() rather than four fake
// `buding-*` ids, which is the honest state — see internal/chatmode. On the
// wire nothing changes: `models` is `[]` and `defaultModel` is `""`.
type chatModeModelJSON struct {
	ID          string `json:"id"`
	CompositeID string `json:"compositeId,omitempty"`
}

type chatModeJSON struct {
	ID           string              `json:"id"`
	Models       []chatModeModelJSON `json:"models"`
	DefaultModel string              `json:"defaultModel"`
}

type chatModesResponse struct {
	Modes []chatModeJSON `json:"modes"`
	// Fallback is true when chat-modes.json was present but unreadable and the
	// built-in default is being served instead (需求 §9: 损坏时用内置默认并保留原文件).
	Fallback bool `json:"fallback"`
}

// handleGetChatModes serves GET /api/product/chat-modes: the mode→model
// grouping the selector renders. The grouping comes from chat-modes.json (the
// user-editable config); each model id is resolved against config.yml's
// endpoint system so the frontend has the composite id it needs to actually
// bind a session to that model.
func (s *Server) handleGetChatModes(w http.ResponseWriter, r *http.Request) {
	path, err := datapath.Join("chat-modes.json")
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	cfg, loadErr := chatmode.Load(path)
	fallback := loadErr != nil

	endpoints, _ := config.Load()

	out := chatModesResponse{Modes: make([]chatModeJSON, 0, len(cfg.Modes)), Fallback: fallback}
	for _, m := range cfg.Modes {
		mj := chatModeJSON{ID: m.ID, DefaultModel: m.DefaultModel, Models: make([]chatModeModelJSON, 0, len(m.Models))}
		for _, id := range m.Models {
			mj.Models = append(mj.Models, chatModeModelJSON{ID: id, CompositeID: compositeIDForModel(endpoints, id)})
		}
		out.Modes = append(out.Modes, mj)
	}
	writeJSON(w, http.StatusOK, out)
}

// compositeIDForModel turns a chat-modes.json model id into the composite id
// the sender layer keys on ("<endpoint>::<model>"). An already-composite id
// passes through verbatim; an id with no matching endpoint maps to "" (the
// model is listed but not yet configured — the selector shows it, the user
// just can't select it until it lands in config.yml).
func compositeIDForModel(cfg config.Config, modelID string) string {
	if modelID == "" {
		return ""
	}
	if _, _, ok := splitComposite(modelID); ok {
		return modelID
	}
	for _, ep := range cfg.Endpoints {
		for _, m := range ep.Models {
			if m.Model == modelID {
				return ep.CompositeID(modelID)
			}
		}
	}
	return ""
}

// splitComposite mirrors config's splitCompositeID ("<endpoint>::<model>") so
// we can recognise an already-composite id without importing that unexported
// helper.
func splitComposite(id string) (string, string, bool) {
	for i := 0; i+1 < len(id); i++ {
		if id[i] == ':' && id[i+1] == ':' {
			return id[:i], id[i+2:], true
		}
	}
	return "", "", false
}

// setChatModeRequest is the PUT /api/sessions/{id}/chat-mode body.
type setChatModeRequest struct {
	Mode string `json:"mode"`
}

// defaultChatMode returns the account's "new session default mode", falling
// back to the "default" group for a legacy state or a nil store (需求 §5.6
// 规则 5: 出厂为分组「默认」).
func (s *Server) defaultChatMode() string {
	if s.productState != nil {
		if m := s.productState.Snapshot().Prefs.DefaultChatMode; m != "" {
			return m
		}
	}
	return chatmode.ModeDefault
}

// handleSetSessionChatMode persists the session's mode group. It mirrors
// handleUpdateSessionModel's binding discipline: reload after acquiring the
// lease, then append a chat_mode record (需求 §5.6 规则 6/7 — only this session
// changes, existing bubbles are untouched). OCTO-FORK: P9.
func (s *Server) handleSetSessionChatMode(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	if id == "" {
		writeError(w, http.StatusBadRequest, "missing session id")
		return
	}

	var req setChatModeRequest
	if err := readBodyJSON(r, &req); err != nil {
		writeInvalidJSONBody(w, err)
		return
	}
	if req.Mode != chatmode.ModePrivacy && req.Mode != chatmode.ModeSmart && req.Mode != chatmode.ModeDefault {
		writeError(w, http.StatusBadRequest, "mode must be privacy, smart, or default")
		return
	}

	sess, err := agent.LoadSession(id)
	if err != nil {
		writeError(w, http.StatusNotFound, err.Error())
		return
	}

	if ok, _, berr := s.acquireSessionBinding(id, agent.EntryWeb, false); !ok {
		writeError(w, http.StatusConflict, berr.Error())
		return
	}
	defer s.releaseSessionBinding(id, agent.EntryWeb)

	sess, err = agent.LoadSession(id)
	if err != nil {
		writeError(w, http.StatusNotFound, err.Error())
		return
	}

	if err := sess.SetChatMode(req.Mode); err != nil {
		writeError(w, http.StatusInternalServerError, "save chat mode: "+err.Error())
		return
	}

	// Push the new mode so this session's composer chip refreshes without
	// waiting for the next turn's session_update — and so other open windows
	// on the same session stay in sync. Only this session changed.
	if s.wsHub != nil {
		s.wsHub.broadcast(id, map[string]any{
			"type":       "session_update",
			"session_id": id,
			"chat_mode":  req.Mode,
		})
	}

	writeJSON(w, http.StatusOK, map[string]any{"ok": true, "chat_mode": req.Mode})
}

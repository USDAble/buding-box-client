package clienttest

import (
	"encoding/json"
	"github.com/open-octo/octo-agent/internal/productclient"
	"net/http"
)

func (s *Server) handleAccountNickname(w http.ResponseWriter, r *http.Request) {
	s.mu.Lock()
	defer s.mu.Unlock()
	phone, ok := s.access[bearer(r)]
	if !ok {
		writeError(w, 401, productclient.CodeUnauthorized, "")
		return
	}
	var req productclient.AccountNickname
	if json.NewDecoder(r.Body).Decode(&req) != nil {
		writeError(w, 400, "nickname_format", "")
		return
	}
	s.accounts[phone].nickname = req.Nickname
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]any{"data": req})
}

func (s *Server) handleAccountIdentity(w http.ResponseWriter, r *http.Request) {
	s.mu.Lock()
	defer s.mu.Unlock()
	phone, ok := s.access[bearer(r)]
	if !ok {
		writeError(w, 401, productclient.CodeUnauthorized, "")
		return
	}
	account := s.accounts[phone]
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]any{"data": map[string]string{"id": account.id, "nickname": account.nickname}})
}

package server

import (
	"encoding/json"
	"net/http"
	"strings"
	"testing"

	"github.com/open-octo/octo-agent/internal/agent"
	"github.com/open-octo/octo-agent/internal/config"
)

func TestProtectionUpdatePersistsPolicyAndModelAtomically(t *testing.T) {
	setTestHome(t)
	sess := agent.NewSession("public", "")
	if err := sess.Save(); err != nil {
		t.Fatal(err)
	}
	srv := mustServer(t, Config{
		Addr: "127.0.0.1:0",
		CatalogModel: func(id string) (CatalogModelStatus, bool) {
			return CatalogModelStatus{Selectable: true, Confidential: id == "private", Current: true}, true
		},
	})

	w := doJSON(t, srv, http.MethodPatch, "/api/sessions/"+sess.ID+"/protection",
		`{"personal_info_protection":true,"confidential_session":true,"model_id":"private"}`)
	if w.Code != http.StatusOK {
		t.Fatalf("PATCH = %d: %s", w.Code, w.Body.String())
	}
	got, err := agent.LoadSession(sess.ID)
	if err != nil {
		t.Fatal(err)
	}
	if !got.ProtectionPolicy.PersonalInfoProtection || !got.ProtectionPolicy.ConfidentialSession || got.ProtectionPolicy.Locked {
		t.Fatalf("policy = %+v", got.ProtectionPolicy)
	}
	if got.Model != "private" || got.ModelConfig != "" {
		t.Fatalf("model = %q model_config = %q", got.Model, got.ModelConfig)
	}
}

func TestCreateSessionPersistsRequestedProtectionWithModel(t *testing.T) {
	setTestHome(t)
	srv := mustServer(t, Config{
		Addr: "127.0.0.1:0",
		CatalogModel: func(id string) (CatalogModelStatus, bool) {
			return CatalogModelStatus{Selectable: true, Confidential: id == "private", Current: true}, true
		},
	})
	w := doJSON(t, srv, http.MethodPost, "/api/sessions",
		`{"model":"private","personal_info_protection":false,"confidential_session":true}`)
	if w.Code != http.StatusOK {
		t.Fatalf("POST = %d: %s", w.Code, w.Body.String())
	}
	var body struct {
		Session struct {
			ID string `json:"id"`
		} `json:"session"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	got, err := agent.LoadSession(body.Session.ID)
	if err != nil {
		t.Fatal(err)
	}
	want := agent.ProtectionPolicy{Version: agent.ProtectionPolicyVersion, ConfidentialSession: true}
	if got.ProtectionPolicy != want || got.Model != "private" {
		t.Fatalf("created session policy=%+v model=%q, want %+v/private", got.ProtectionPolicy, got.Model, want)
	}
}

func TestAcceptedFirstTurnLocksProtectionPolicy(t *testing.T) {
	setTestHome(t)
	srv := mustServer(t, Config{Addr: "127.0.0.1:0", Tools: false})
	w := doJSON(t, srv, http.MethodPost, "/api/chat", `{"message":"hello"}`)
	if w.Code != http.StatusOK {
		t.Fatalf("POST turn = %d: %s", w.Code, w.Body.String())
	}
	var body struct {
		SessionID string `json:"session_id"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	got, err := agent.LoadSession(body.SessionID)
	if err != nil {
		t.Fatal(err)
	}
	if !got.ProtectionPolicy.Locked || !got.ProtectionPolicy.PersonalInfoProtection {
		t.Fatalf("first accepted turn policy = %+v", got.ProtectionPolicy)
	}
}

func TestProtectionUpdateFailureLeavesOldState(t *testing.T) {
	setTestHome(t)
	sess := agent.NewSession("public", "")
	if err := sess.Save(); err != nil {
		t.Fatal(err)
	}
	srv := mustServer(t, Config{
		Addr: "127.0.0.1:0",
		CatalogModel: func(string) (CatalogModelStatus, bool) {
			return CatalogModelStatus{Selectable: true, Current: true}, true
		},
	})

	w := doJSON(t, srv, http.MethodPatch, "/api/sessions/"+sess.ID+"/protection",
		`{"personal_info_protection":false,"confidential_session":true,"model_id":"not-private"}`)
	if w.Code != http.StatusConflict {
		t.Fatalf("PATCH = %d, want 409: %s", w.Code, w.Body.String())
	}
	if !strings.Contains(w.Body.String(), `"code":"confidential_model_required"`) {
		t.Fatalf("missing stable error code: %s", w.Body.String())
	}
	got, err := agent.LoadSession(sess.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got.ProtectionPolicy != agent.DefaultProtectionPolicy() || got.Model != "public" {
		t.Fatalf("failed update changed state: policy=%+v model=%q", got.ProtectionPolicy, got.Model)
	}
}

func TestProtectionUpdateAutoSelectsPreferredConfidentialModel(t *testing.T) {
	setTestHome(t)
	sess := agent.NewSession("public", "")
	if err := sess.Save(); err != nil {
		t.Fatal(err)
	}
	srv := mustServer(t, Config{
		Addr: "127.0.0.1:0",
		CatalogModel: func(id string) (CatalogModelStatus, bool) {
			return CatalogModelStatus{Selectable: true, Confidential: id == "private", Current: true}, true
		},
		PreferredConfidentialModel: func() (string, bool) { return "private", true },
	})

	w := doJSON(t, srv, http.MethodPatch, "/api/sessions/"+sess.ID+"/protection",
		`{"personal_info_protection":true,"confidential_session":true}`)
	if w.Code != http.StatusOK {
		t.Fatalf("PATCH = %d: %s", w.Code, w.Body.String())
	}
	got, err := agent.LoadSession(sess.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got.Model != "private" || !got.ProtectionPolicy.ConfidentialSession {
		t.Fatalf("model=%q policy=%+v", got.Model, got.ProtectionPolicy)
	}
}

func TestConfidentialSessionCannotSwitchToOrdinaryModel(t *testing.T) {
	setTestHome(t)
	sess := agent.NewSession("private", "")
	policy := agent.DefaultProtectionPolicy()
	policy.ConfidentialSession = true
	if err := sess.SetProtectionPolicyAndModel(policy, "", "private", false); err != nil {
		t.Fatal(err)
	}
	sess.LockProtectionPolicy()
	if err := sess.Save(); err != nil {
		t.Fatal(err)
	}
	srv := mustServer(t, Config{
		Addr: "127.0.0.1:0",
		CatalogModel: func(id string) (CatalogModelStatus, bool) {
			return CatalogModelStatus{Selectable: true, Confidential: id == "private", Current: true}, true
		},
	})
	w := doJSON(t, srv, http.MethodPatch, "/api/sessions/"+sess.ID+"/model", `{"model_id":"ordinary"}`)
	if w.Code != http.StatusConflict || !strings.Contains(w.Body.String(), `"code":"confidential_model_required"`) {
		t.Fatalf("PATCH model = %d: %s", w.Code, w.Body.String())
	}
	got, err := agent.LoadSession(sess.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got.Model != "private" {
		t.Fatalf("refused switch changed model to %q", got.Model)
	}
}

func TestProtectionUpdateRejectsLockedSession(t *testing.T) {
	setTestHome(t)
	sess := agent.NewSession("public", "")
	sess.LockProtectionPolicy()
	sess.Messages = append(sess.Messages, agent.NewUserMessage("first"))
	if err := sess.Save(); err != nil {
		t.Fatal(err)
	}
	srv := mustServer(t, Config{Addr: "127.0.0.1:0"})

	w := doJSON(t, srv, http.MethodPatch, "/api/sessions/"+sess.ID+"/protection",
		`{"personal_info_protection":false,"confidential_session":false}`)
	if w.Code != http.StatusConflict {
		t.Fatalf("PATCH = %d, want 409: %s", w.Code, w.Body.String())
	}
	if !strings.Contains(w.Body.String(), `"code":"session_policy_locked"`) {
		t.Fatalf("missing stable error code: %s", w.Body.String())
	}
	got, err := agent.LoadSession(sess.ID)
	if err != nil {
		t.Fatal(err)
	}
	if !got.ProtectionPolicy.Locked || !got.ProtectionPolicy.PersonalInfoProtection {
		t.Fatalf("locked policy changed: %+v", got.ProtectionPolicy)
	}
}

func TestProtectionUpdateTreatsInFlightFirstTurnAsLocked(t *testing.T) {
	setTestHome(t)
	sess := agent.NewSession("public", "")
	if err := sess.Save(); err != nil {
		t.Fatal(err)
	}
	srv := mustServer(t, Config{Addr: "127.0.0.1:0"})
	turnMu := srv.sessionTurnLock(sess.ID)
	turnMu.Lock()
	if srv.turnRunning == nil {
		srv.turnRunning = make(map[string]bool)
	}
	srv.turnRunning[sess.ID] = true
	turnMu.Unlock()

	w := doJSON(t, srv, http.MethodPatch, "/api/sessions/"+sess.ID+"/protection",
		`{"personal_info_protection":false,"confidential_session":false}`)
	if w.Code != http.StatusConflict {
		t.Fatalf("PATCH = %d, want 409: %s", w.Code, w.Body.String())
	}
	if !strings.Contains(w.Body.String(), `"code":"session_policy_locked"`) {
		t.Fatalf("in-flight turn must expose stable lock code: %s", w.Body.String())
	}
	got, err := agent.LoadSession(sess.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got.ProtectionPolicy != agent.DefaultProtectionPolicy() {
		t.Fatalf("rejected concurrent update changed policy: %+v", got.ProtectionPolicy)
	}
}

func TestConfidentialQualificationIsRecheckedBeforeEveryTurn(t *testing.T) {
	setTestHome(t)
	sess := agent.NewSession("private", "")
	policy := agent.DefaultProtectionPolicy()
	policy.ConfidentialSession = true
	if err := sess.SetProtectionPolicyAndModel(policy, "", "private", false); err != nil {
		t.Fatal(err)
	}
	sess.LockProtectionPolicy()
	if err := sess.Save(); err != nil {
		t.Fatal(err)
	}
	eligible := true
	srv := mustServer(t, Config{
		Addr: "127.0.0.1:0", Tools: false,
		CatalogModel: func(string) (CatalogModelStatus, bool) {
			return CatalogModelStatus{Selectable: true, Confidential: eligible, Current: true}, true
		},
	})
	eligible = false

	w := doJSON(t, srv, http.MethodPost, "/api/chat/"+sess.ID+"/turn", `{"message":"secret"}`)
	if w.Code != http.StatusConflict {
		t.Fatalf("turn = %d, want 409 refusal: %s", w.Code, w.Body.String())
	}
	if !strings.Contains(w.Body.String(), `"code":"confidential_model_unavailable"`) {
		t.Fatalf("missing stable error code: %s", w.Body.String())
	}
	got, err := agent.LoadSession(sess.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(got.Messages) != 0 || !got.ProtectionPolicy.ConfidentialSession || !got.ProtectionPolicy.Locked {
		t.Fatalf("refused turn changed session: messages=%d policy=%+v", len(got.Messages), got.ProtectionPolicy)
	}
}

func TestProductProfileDoesNotQualifyLocalConfidentialModel(t *testing.T) {
	setTestHome(t)
	seedModels(t, config.Config{Endpoints: []config.Endpoint{{
		ID: "local", Provider: "openai", Models: []config.EndpointModel{{Model: "private", Confidential: true}},
	}}})
	srv := mustServer(t, Config{Addr: "127.0.0.1:0", RequireGateway: true})
	if srv.confidentialModelEligible("local::private") {
		t.Fatal("product profile must not derive confidential eligibility from local config")
	}
	modelConfig, model, err := srv.resolveSessionModel("local::private")
	if err == nil {
		t.Fatalf("product resolution accepted local config: model_config=%q model=%q", modelConfig, model)
	}
}

func TestDeveloperProfileKeepsLocalConfidentialModelSeparateFromCatalog(t *testing.T) {
	setTestHome(t)
	seedModels(t, config.Config{Endpoints: []config.Endpoint{{
		ID: "deepseek", Provider: "openai", Models: []config.EndpointModel{{Model: "deepseek-flash", Confidential: true}},
	}}})
	// OCTO-FORK: a valid catalog deliberately reports an unlisted local model as
	// ineligible. The developer path must still use the locally configured mark.
	srv := mustServer(t, Config{
		Addr:               "127.0.0.1:0",
		GatewayModelPrefix: "buding-gateway::",
		CatalogModel: func(string) (CatalogModelStatus, bool) {
			return CatalogModelStatus{Current: true}, true
		},
	})
	if !srv.confidentialModelEligible("deepseek::deepseek-flash") {
		t.Fatal("developer-local confidential model was rejected by an unrelated catalog")
	}
	if srv.confidentialModelEligible("buding-gateway::unlisted") {
		t.Fatal("gateway model must not inherit a local endpoint's confidential mark")
	}
}

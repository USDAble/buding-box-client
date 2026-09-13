package agent

import (
	"os"
	"testing"
)

func TestSetChatMode_AppendsAndReloads(t *testing.T) {
	// 需求基线 B5 规则 6: the session-level mode is a session attribute, persisted
	// on the session — so the assertion is made against the file, not the value
	// still sitting in memory.
	setTempHome(t)

	s := NewSession("m", "")
	s.Messages = []Message{NewUserMessage("ping")}
	if err := s.Save(); err != nil {
		t.Fatal(err)
	}
	if err := s.SetChatMode("privacy"); err != nil {
		t.Fatalf("SetChatMode: %v", err)
	}

	got, err := LoadSession(s.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got.ChatMode != "privacy" {
		t.Errorf("ChatMode after reload = %q, want %q", got.ChatMode, "privacy")
	}
	if len(got.Messages) != 1 {
		t.Errorf("Messages len = %d, want 1 (the mode record must not disturb history)", len(got.Messages))
	}
}

func TestSetChatMode_SameValueWritesNothing(t *testing.T) {
	// Same rule as SetPermissionMode: setting the mode already in place is a
	// no-op. Without it every menu click that lands on the current row appends
	// another record to the transcript.
	setTempHome(t)

	s := NewSession("m", "")
	s.Messages = []Message{NewUserMessage("ping")}
	if err := s.Save(); err != nil {
		t.Fatal(err)
	}
	if err := s.SetChatMode("privacy"); err != nil {
		t.Fatal(err)
	}
	path, err := s.SavePath()
	if err != nil {
		t.Fatal(err)
	}
	before, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}

	if err := s.SetChatMode("privacy"); err != nil {
		t.Fatalf("SetChatMode(same): %v", err)
	}

	after, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if string(before) != string(after) {
		t.Errorf("setting the same chat mode changed the file:\nbefore=%s\nafter=%s", before, after)
	}
}

func TestSetChatMode_SurvivesRewrite(t *testing.T) {
	// A full rewrite (compaction path) folds the mode into the meta header.
	// Without it, compacting a privacy-mode session would silently drop it back
	// to the account default.
	setTempHome(t)

	s := NewSession("m", "")
	s.Messages = []Message{NewUserMessage("ping"), NewAssistantMessage("pong")}
	if err := s.Save(); err != nil {
		t.Fatal(err)
	}
	if err := s.SetChatMode("privacy"); err != nil {
		t.Fatal(err)
	}
	s.forceRewrite = true
	if err := s.Save(); err != nil {
		t.Fatal(err)
	}

	got, err := LoadSession(s.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got.ChatMode != "privacy" {
		t.Errorf("after rewrite ChatMode = %q, want privacy", got.ChatMode)
	}
}

func TestSetChatMode_BeforeSave(t *testing.T) {
	// A mode set before the first Save just rides the meta header, like the
	// working dir and the permission mode.
	setTempHome(t)

	s := NewSession("m", "")
	s.Messages = []Message{NewUserMessage("ping")}
	if err := s.SetChatMode("smart"); err != nil {
		t.Fatal(err)
	}
	if err := s.Save(); err != nil {
		t.Fatal(err)
	}

	got, err := LoadSession(s.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got.ChatMode != "smart" {
		t.Errorf("ChatMode after first Save = %q, want smart", got.ChatMode)
	}
}

func TestSetChatMode_NeverCreatesOrphanFile(t *testing.T) {
	// A session that has never been saved must carry the value until its first
	// Save, not materialise a file holding nothing but a mode record — those
	// show up as ghost sessions in the sidebar (SetTitle/SetWorkingDir rule).
	setTempHome(t)

	s := NewSession("m", "")
	if err := s.SetChatMode("smart"); err != nil {
		t.Fatalf("SetChatMode before Save: %v", err)
	}
	if s.ChatMode != "smart" {
		t.Fatalf("ChatMode = %q, want smart (carried in memory)", s.ChatMode)
	}
	path, err := s.SavePath()
	if err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Fatalf("SetChatMode created an orphan mode-only file at %s", path)
	}
}

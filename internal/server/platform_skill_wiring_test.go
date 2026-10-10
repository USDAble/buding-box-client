package server

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/open-octo/octo-agent/internal/agent"
	"github.com/open-octo/octo-agent/internal/skills"
)

// OCTO-FORK: platform sessions must not advertise skills without an authenticated loader.
func TestPreparePlatformToolTurnRequiresLoader(t *testing.T) {
	s := &Server{}
	ctx := context.WithValue(context.Background(), ctxKeySessionID{}, "session")
	_, _, _, cleanup, err := s.prepareToolTurn(ctx, nil, &agent.Session{AgentID: "platform:expert_test:7"})
	cleanup()
	if err == nil || !strings.Contains(err.Error(), "platform skill loader unavailable") {
		t.Fatalf("expected missing platform loader error, got %v", err)
	}
}

func TestPlatformExpertCanDiscoverLocalSkills(t *testing.T) {
	root := t.TempDir()
	t.Setenv("OCTO_DATA_ROOT", root)
	dir := filepath.Join(root, "skills", "local-writing")
	if err := os.MkdirAll(dir, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "SKILL.md"), []byte("---\nname: local-writing\ndescription: Write a local document\n---\nLOCAL_SKILL_INSTRUCTIONS"), 0644); err != nil {
		t.Fatal(err)
	}
	s := &Server{skillReg: skills.Discover()}
	manifest := s.curSkillsManifestForProfile(s.profileForAgent("platform:translator:1"))
	if !strings.Contains(manifest, "local-writing") {
		t.Fatalf("platform expert lost local skills: %s", manifest)
	}
}

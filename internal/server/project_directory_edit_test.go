package server

import (
	"net/http"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/open-octo/octo-agent/internal/agent"
)

func TestProjectDirectory_EditPersistsAndPreservesFiles(t *testing.T) {
	srv := groupTestServer(t)
	gid, original := newProjectGroupWS(t, srv, "Work", "")
	wantMemory, err := projectGroupByID(t, gid).projectRef().MemoryDir()
	if err != nil {
		t.Fatal(err)
	}
	target := t.TempDir()
	file := filepath.Join(target, "keep.txt")
	if err := os.WriteFile(file, []byte("user content"), 0o600); err != nil {
		t.Fatal(err)
	}
	rec, out := doGroupReq(t, srv, http.MethodPatch, "/api/session-groups/"+gid, map[string]any{"working_dir": target})
	if rec.Code != http.StatusOK {
		t.Fatalf("edit: %d %s", rec.Code, rec.Body.String())
	}
	g := out["group"].(map[string]any)
	if g["working_dir"] != target || g["working_dir_locked"] != false || g["workspace_dir"] != original {
		t.Fatalf("edited group: %v", g)
	}
	srv.migrateProjectWorkspaces()
	reloaded := projectGroupByID(t, gid)
	if reloaded.WorkingDir != target || reloaded.WorkspaceDir != original || len(reloaded.SourceDirs) != 0 {
		t.Fatalf("startup changed explicit directory: %+v", reloaded)
	}
	// A new session inherits the edited directory, including after a reload.
	sess := saveSessionWithDir(t, "")
	fileInProject(t, gid, sess.ID)
	if got := srv.sessionCwd(sess); got != target {
		t.Fatalf("new session cwd = %q, want %q", got, target)
	}
	if got := ProjectMemoryDirForSession(sess.ID); got != wantMemory {
		t.Fatalf("resumed session memory = %q, want %q", got, wantMemory)
	}
	claims := ProjectsClaimingDir(target)
	if len(claims) != 1 {
		t.Fatalf("edited directory claims: %+v", claims)
	}
	if got, err := claims[0].MemoryDir(); err != nil || got != wantMemory {
		t.Fatalf("CLI memory = %q, %v; want %q", got, err, wantMemory)
	}
	if got := srv.sessionMemDir(&reloaded); got != wantMemory {
		t.Fatalf("server memory = %q, want %q", got, wantMemory)
	}
	// A deleted transcript may leave a registry ID; it must not lock an
	// otherwise pristine project.
	if err := addSessionToGroup(gid, "deleted-session"); err != nil {
		t.Fatal(err)
	}
	if rec, out := doGroupReq(t, srv, http.MethodGet, "/api/session-groups", nil); rec.Code != http.StatusOK || out["groups"].([]any)[0].(map[string]any)["working_dir_locked"] != false {
		t.Fatalf("stale member locked project: %d %s", rec.Code, rec.Body.String())
	}
	// Remove the fixture's stale ID before exercising real session deletion.
	groupMu.LockWrite()
	groups, loadErr := loadSessionGroups()
	if loadErr == nil {
		for i := range groups {
			if groups[i].ID == gid {
				groups[i].SessionIDs = []string{sess.ID}
			}
		}
		loadErr = saveSessionGroups(groups)
	}
	groupMu.Unlock()
	if loadErr != nil {
		t.Fatal(loadErr)
	}
	if rec, _ := doGroupReq(t, srv, http.MethodDelete, "/api/session-groups/"+gid+"?sessions=delete", nil); rec.Code != http.StatusOK {
		t.Fatalf("delete project: %d", rec.Code)
	}
	if data, err := os.ReadFile(file); err != nil || string(data) != "user content" {
		t.Fatalf("project deletion changed user files: %q %v", data, err)
	}
}

func projectGroupByID(t *testing.T, id string) sessionGroup {
	t.Helper()
	groupMu.Lock()
	defer groupMu.Unlock()
	groups, err := loadSessionGroups()
	if err != nil {
		t.Fatal(err)
	}
	for _, g := range groups {
		if g.ID == id {
			return g
		}
	}
	t.Fatalf("project %s not found", id)
	return sessionGroup{}
}

func TestProjectDirectory_LocksForEveryMember(t *testing.T) {
	for _, state := range []string{"message", "accepted", "running", "busy", "lease", "unreadable"} {
		t.Run(state, func(t *testing.T) {
			srv := groupTestServer(t)
			gid, original := newProjectGroupWS(t, srv, "Work", "")
			empty := saveSessionWithDir(t, "")
			used := saveSessionWithDir(t, "")
			fileInProject(t, gid, empty.ID)
			fileInProject(t, gid, used.ID)
			switch state {
			case "message":
				used.Messages = []agent.Message{{Role: "user", Content: "hello"}}
			case "accepted":
				used.LockProtectionPolicy()
			case "running":
				mu := srv.sessionTurnLock(used.ID)
				mu.Lock()
				if srv.turnRunning == nil {
					srv.turnRunning = make(map[string]bool)
				}
				srv.turnRunning[used.ID] = true
				mu.Unlock()
			case "busy":
				mu := srv.sessionTurnLock(used.ID)
				mu.Lock()
				defer mu.Unlock()
			case "lease":
				if err := used.WriteLease(agent.EntryCLI, time.Now().Add(time.Minute)); err != nil {
					t.Fatal(err)
				}
			}
			if err := used.Save(); err != nil {
				t.Fatal(err)
			}
			if state == "unreadable" {
				dir, err := agent.SessionsDir()
				if err != nil {
					t.Fatal(err)
				}
				if err := os.WriteFile(filepath.Join(dir, used.ID+".jsonl"), []byte("not json\n"), 0o600); err != nil {
					t.Fatal(err)
				}
			}
			rec, out := doGroupReq(t, srv, http.MethodPatch, "/api/session-groups/"+gid, map[string]any{
				"working_dir": t.TempDir(), "name": "must not change",
			})
			if rec.Code != http.StatusConflict || out["code"] != "project_directory_locked" {
				t.Fatalf("edit locked project: %d %s", rec.Code, rec.Body.String())
			}
			g := projectGroupByID(t, gid)
			if g.WorkingDir != original || g.Name != "Work" {
				t.Fatalf("rejected edit changed registry: %+v", g)
			}
			rec, out = doGroupReq(t, srv, http.MethodGet, "/api/session-groups", nil)
			listed := out["groups"].([]any)[0].(map[string]any)
			if rec.Code != http.StatusOK || listed["working_dir_locked"] != true {
				t.Fatalf("list must report locked: %d %v", rec.Code, listed)
			}
		})
	}
}

func TestProjectDirectory_RejectsInvalidTargets(t *testing.T) {
	srv := groupTestServer(t)
	gid, original := newProjectGroupWS(t, srv, "Work", "")
	otherID, otherWorkspace := newProjectGroupWS(t, srv, "Other", "")
	file := filepath.Join(t.TempDir(), "file")
	if err := os.WriteFile(file, []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}
	for _, path := range []string{"", "   ", filepath.Join(t.TempDir(), "missing"), file, otherWorkspace, srv.curWorkspaceDir()} {
		rec, _ := doGroupReq(t, srv, http.MethodPatch, "/api/session-groups/"+gid, map[string]any{"working_dir": path})
		if rec.Code != http.StatusBadRequest {
			t.Fatalf("target %q: %d %s", path, rec.Code, rec.Body.String())
		}
		if got := projectGroupByID(t, gid).WorkingDir; got != original {
			t.Fatalf("invalid target changed cwd: %q", got)
		}
	}
	// An external directory already chosen by another project is also owned.
	shared := t.TempDir()
	if rec, _ := doGroupReq(t, srv, http.MethodPatch, "/api/session-groups/"+otherID, map[string]any{"working_dir": shared}); rec.Code != http.StatusOK {
		t.Fatalf("set other cwd: %d", rec.Code)
	}
	if rec, _ := doGroupReq(t, srv, http.MethodPatch, "/api/session-groups/"+gid, map[string]any{"working_dir": shared}); rec.Code != http.StatusBadRequest {
		t.Fatalf("shared cwd: %d", rec.Code)
	}
}

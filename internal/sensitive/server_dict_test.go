// OCTO-FORK: L-D5 server dictionary acceptance nails — see the product baseline D4/D5.
package sensitive_test

import (
	"bytes"
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/open-octo/octo-agent/internal/sensitive"
)

func openServerStore(t *testing.T) (*sensitive.ServerStore, string) {
	t.Helper()
	root := t.TempDir()
	t.Setenv("OCTO_DATA_ROOT", root)
	store, err := sensitive.OpenServerStore()
	if err != nil {
		t.Fatalf("OpenServerStore: %v", err)
	}
	return store, filepath.Join(root, sensitive.ServerDictFileName)
}

func serverEntry(version string, words ...string) sensitive.ServerEntry {
	return sensitive.ServerEntry{
		Version:   version,
		KeyID:     "dictionary-key",
		Signature: "signature",
		Words:     words,
		FetchedAt: time.Date(2026, 9, 15, 8, 0, 0, 0, time.UTC),
	}
}

func TestServerStoreDoesNotCreateAFileUntilAValidatedEntryIsPut(t *testing.T) {
	store, path := openServerStore(t)
	if _, err := os.Stat(path); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("cache exists after OpenServerStore: %v", err)
	}
	if _, err := store.Load(); !errors.Is(err, sensitive.ErrNoServerDictionary) {
		t.Fatalf("Load error = %v, want ErrNoServerDictionary", err)
	}
}

func TestServerStoreKeepsTheNewestEntryAndDoesNotRewriteAnEqualVersion(t *testing.T) {
	store, path := openServerStore(t)
	if err := store.Put(serverEntry("43", "server-word")); err != nil {
		t.Fatalf("Put v43: %v", err)
	}
	original, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read cache: %v", err)
	}

	if err := store.Put(serverEntry("43", "different-word")); err != nil {
		t.Fatalf("Put equal version: %v", err)
	}
	afterEqual, _ := os.ReadFile(path)
	if !bytes.Equal(afterEqual, original) {
		t.Fatal("an equal version rewrote the cache")
	}

	err = store.Put(serverEntry("42", "older-word"))
	if !errors.Is(err, sensitive.ErrServerDictionaryRolledBack) {
		t.Fatalf("Put rollback error = %v", err)
	}
	afterRollback, _ := os.ReadFile(path)
	if !bytes.Equal(afterRollback, original) {
		t.Fatal("a rolled-back version changed the cache")
	}
}

func TestServerStoreRejectsInvalidWordsWithoutChangingTheCache(t *testing.T) {
	store, path := openServerStore(t)
	if err := store.Put(serverEntry("43", "server-word")); err != nil {
		t.Fatalf("Put valid entry: %v", err)
	}
	original, _ := os.ReadFile(path)

	err := store.Put(serverEntry("44", "！@#"))
	if !errors.Is(err, sensitive.ErrInvalidServerDictionary) {
		t.Fatalf("Put invalid word error = %v", err)
	}
	after, _ := os.ReadFile(path)
	if !bytes.Equal(after, original) {
		t.Fatal("an invalid update changed the cache")
	}
}

func TestEngineMergesBuiltinUserAndServerWords(t *testing.T) {
	store, _ := openServerStore(t)
	if err := store.Put(serverEntry("43", "server-only")); err != nil {
		t.Fatalf("Put: %v", err)
	}
	userPath := filepath.Join(t.TempDir(), sensitive.DictFileName)
	if err := os.WriteFile(userPath, []byte("user-only\n"), 0o600); err != nil {
		t.Fatalf("write user dictionary: %v", err)
	}

	engine := sensitive.NewWithServer(userPath, store)
	for _, word := range []string{"发票", "user-only", "server-only"} {
		if got := engine.Filter("x" + word + "y").Text; got != "x***y" {
			t.Errorf("Filter(%q) = %q", word, got)
		}
	}

	if err := os.WriteFile(userPath, []byte{0xff}, 0o600); err != nil {
		t.Fatalf("damage user dictionary: %v", err)
	}
	if got := engine.Filter("server-only").Text; got != sensitive.Mask {
		t.Errorf("server layer stopped after user-layer damage: %q", got)
	}
}

func TestEnginePicksUpANewerServerEntryWithoutRestart(t *testing.T) {
	store, _ := openServerStore(t)
	engine := sensitive.NewWithServer("", store)
	if got := engine.Filter("new-server-word").Text; got != "new-server-word" {
		t.Fatalf("word matched before sync: %q", got)
	}
	if err := store.Put(serverEntry("1", "new-server-word")); err != nil {
		t.Fatalf("Put: %v", err)
	}
	if got := engine.Filter("new-server-word").Text; got != sensitive.Mask {
		t.Errorf("new server entry did not hot-load: %q", got)
	}
}

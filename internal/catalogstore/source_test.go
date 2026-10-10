package catalogstore_test

import (
	"bytes"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/open-octo/octo-agent/internal/catalogstore"
)

func TestSourceCachesPreserveIndependentRollbackHighWaterMarks(t *testing.T) {
	production, root := openStore(t)
	key, _ := signingPair(t)
	// Use the deployed date-and-counter contract, including padded counters.
	const highVersion = "2026-09-20.00000000000000000121"
	const lowVersion = "2026-09-20.00000000000000000075"
	const olderVersion = "2026-09-20.00000000000000000074"
	high := entry(t, key, policyBytes(t, highVersion), highVersion)
	low := entry(t, key, policyBytes(t, lowVersion), lowVersion)
	if err := production.Put(high); err != nil {
		t.Fatal(err)
	}
	original := readRaw(t, cachePath(root))
	// Other user data stays untouched across source changes.
	sentinel := filepath.Join(root, "unrelated-user-data")
	if err := os.WriteFile(sentinel, []byte("preserve"), 0600); err != nil {
		t.Fatal(err)
	}
	source := "http://127.0.0.1:8000/api/v1"
	local, err := catalogstore.OpenForSource(source)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = local.Load(); !errors.Is(err, catalogstore.ErrNoCache) {
		t.Fatalf("development inherited deployment cache: %v", err)
	}
	if err = local.Put(low); err != nil {
		t.Fatalf("local version rejected by foreign high water: %v", err)
	}
	restarted, err := catalogstore.OpenForSource(source)
	if err != nil {
		t.Fatal(err)
	}
	cached, err := restarted.Load()
	if err != nil || cached.CatalogVersion != lowVersion || !bytes.Equal(cached.Envelope.Policy, low.Envelope.Policy) {
		t.Fatalf("local restart: %+v, %v", cached, err)
	}
	older := entry(t, key, policyBytes(t, olderVersion), olderVersion)
	if err = restarted.Put(older); !errors.Is(err, catalogstore.ErrRolledBack) {
		t.Fatalf("source rollback no longer protected: %v", err)
	}
	for _, other := range []string{"http://127.0.0.1:8001/api/v1", "http://127.0.0.1:8000/other/v1"} {
		independent, err := catalogstore.OpenForSource(other)
		if err != nil {
			t.Fatal(err)
		}
		if _, err = independent.Load(); !errors.Is(err, catalogstore.ErrNoCache) {
			t.Fatalf("other source inherited cache: %v", err)
		}
	}
	restored, err := catalogstore.Open()
	if err != nil {
		t.Fatal(err)
	}
	cached, err = restored.Load()
	if err != nil || cached.CatalogVersion != highVersion {
		t.Fatalf("deployment high-water mark lost: %+v, %v", cached, err)
	}
	if err = restored.Put(low); !errors.Is(err, catalogstore.ErrRolledBack) {
		t.Fatalf("deployment rollback no longer protected: %v", err)
	}
	if !bytes.Equal(readRaw(t, cachePath(root)), original) {
		t.Fatal("development changed production cache bytes")
	}
	if string(readRaw(t, sentinel)) != "preserve" {
		t.Fatal("user data changed")
	}
}

func TestOpeningSourceCacheDoesNotWrite(t *testing.T) {
	_, root := openStore(t)
	if _, err := catalogstore.OpenForSource("http://127.0.0.1:8000/api/v1"); err != nil {
		t.Fatal(err)
	}
	entries, err := os.ReadDir(root)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 0 {
		t.Fatal("opening an empty source wrote files")
	}
}

package catalogstore_test

import (
	"bytes"
	"crypto/ed25519"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/open-octo/octo-agent/internal/catalogstore"
	"github.com/open-octo/octo-agent/internal/productclient"
)

// The cache is the one place a signed catalog survives a network outage, and
// the only reason it can be trusted offline is that the bytes the platform
// signed are the bytes that came back off the disk. Everything here is about
// that property and about what happens when it cannot hold.

const (
	testCatalogVersion = "2026-09-12.1"
	testKeyID          = "catalog-test-key"
	testAudience       = "puddingbox"
)

// signingPair is a throwaway key. The store never verifies anything - signature
// checking belongs to productclient - but the round-trip test below does, and it
// must verify against the key that signed, not against a fixture the store
// happens to share with the stand-in platform.
func signingPair(t *testing.T) (ed25519.PrivateKey, string) {
	t.Helper()
	pub, priv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatalf("GenerateKey: %v", err)
	}
	return priv, base64.StdEncoding.EncodeToString(pub)
}

// policyBytes marshals a policy whose display names are hostile to a naive
// writer: `&` and `<` are exactly what encoding/json escapes by default, and a
// cache written that way no longer verifies. Non-ASCII, quotes and backslashes
// are included because they are the other shapes a byte-exact writer must keep.
//
// The encoder escapes HTML off on purpose: with the default on, the `&` would
// already be `\u0026` in these bytes and the test would prove nothing about the
// cache's own writer.
func policyBytes(t *testing.T, version string) []byte {
	t.Helper()
	var buf bytes.Buffer
	enc := json.NewEncoder(&buf)
	enc.SetEscapeHTML(false)
	// No indent: the platform's signed bytes are compact, and a pretty-printed
	// policy is a different, much easier case (see
	// TestPutRefusesAPolicyWhoseBytesWouldNotSurvive).
	if err := enc.Encode(productclient.Policy{
		PolicyVersion: version,
		IssuedAt:      time.Now().UTC().Add(-time.Minute).Format(time.RFC3339),
		ExpiresAt:     time.Now().UTC().Add(time.Hour).Format(time.RFC3339),
		Audience:      testAudience,
		KeyID:         testKeyID,
		Catalog: productclient.Catalog{
			Version: version,
			TTLSec:  3600,
			Models: []productclient.CatalogModel{{
				ID:          "buding-cloud-pro",
				DisplayName: productclient.DisplayName{Zh: "布丁「专业版」", En: `Tools & Agents <beta> "x"\y`},
			}},
		},
	}); err != nil {
		t.Fatalf("marshal policy: %v", err)
	}
	return bytes.TrimRight(buf.Bytes(), "\n")
}

func envelope(t *testing.T, raw []byte, priv ed25519.PrivateKey) productclient.PolicyEnvelope {
	t.Helper()
	sig, err := productclient.SignPolicy(raw, testKeyID, priv)
	if err != nil {
		t.Fatalf("SignPolicy: %v", err)
	}
	return productclient.PolicyEnvelope{Policy: json.RawMessage(raw), Signature: sig}
}

// entry is a complete, well-formed cache entry for raw's policy bytes.
func entry(t *testing.T, priv ed25519.PrivateKey, raw []byte, version string) catalogstore.Entry {
	t.Helper()
	policy := productclient.Policy{}
	if err := json.Unmarshal(raw, &policy); err != nil {
		t.Fatalf("unmarshal policy: %v", err)
	}
	issued, _ := time.Parse(time.RFC3339, policy.IssuedAt)
	expires, _ := time.Parse(time.RFC3339, policy.ExpiresAt)
	// The earlier of the two axes, as 需求基线 B2 规则 1 now spells out.
	if ttl := issued.Add(time.Duration(policy.Catalog.TTLSec) * time.Second); ttl.Before(expires) {
		expires = ttl
	}
	return catalogstore.Entry{
		CatalogVersion: version,
		FetchedAt:      time.Now().UTC(),
		ExpiresAt:      expires,
		KeyID:          testKeyID,
		Audience:       testAudience,
		Envelope:       envelope(t, raw, priv),
	}
}

// openStore points the store at a fresh temp data root, the way the runtime
// does through internal/datapath.
func openStore(t *testing.T) (*catalogstore.Store, string) {
	t.Helper()
	return openStoreSeeded(t, "")
}

// openStoreSeeded writes the given body to the cache path BEFORE opening, which
// is the only order that exercises the parse path: Open reads the file once, and
// the store then owns it (see the package comment on Store).
func openStoreSeeded(t *testing.T, body string) (*catalogstore.Store, string) {
	t.Helper()
	root := t.TempDir()
	t.Setenv("OCTO_DATA_ROOT", root)
	if body != "" {
		if err := os.WriteFile(filepath.Join(root, "catalog.json"), []byte(body), 0o600); err != nil {
			t.Fatalf("seed catalog.json: %v", err)
		}
	}
	s, err := catalogstore.Open(catalogstore.Options{})
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	return s, root
}

func cachePath(root string) string { return filepath.Join(root, "catalog.json") }

func readRaw(t *testing.T, path string) []byte {
	t.Helper()
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}
	return raw
}

// TestOpenDoesNotCreateTheFile is 开发规范 §3.9.1 on this file: absence means
// "no cache yet", and materialising an empty one at startup would both write a
// user-editable file before the user did anything and make "there is no cache"
// indistinguishable from "there is an empty one".
func TestOpenDoesNotCreateTheFile(t *testing.T) {
	_, root := openStore(t)

	if _, err := os.Stat(cachePath(root)); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("catalog.json exists after Open (%v); Open must not write", err)
	}
}

// TestLoadWithoutAFileReportsNoCache pins the absence signal: a caller must be
// able to tell "no catalog" from "a broken catalog", because the two lead to
// different user-visible states (需求基线 B4).
func TestLoadWithoutAFileReportsNoCache(t *testing.T) {
	s, _ := openStore(t)

	_, err := s.Load()
	if !errors.Is(err, catalogstore.ErrNoCache) {
		t.Fatalf("Load error = %v, want ErrNoCache", err)
	}
}

// TestPutThenLoadSurvivesAHostilePolicyByteForByte is the heart of this package.
//
// A cached catalog is only worth anything if it still verifies after a restart,
// and it verifies only if the bytes come back exactly as the platform signed
// them. The hostile policy above contains `&` and `<` precisely because
// encoding/json escapes those by default: a cache written with json.Marshal
// would come back as `\u0026` and `\u003c`, and every offline verification would
// fail - on a machine with no way to tell why.
func TestPutThenLoadSurvivesAHostilePolicyByteForByte(t *testing.T) {
	s, _ := openStore(t)
	priv, pub := signingPair(t)
	raw := policyBytes(t, testCatalogVersion)

	if err := s.Put(entry(t, priv, raw, testCatalogVersion)); err != nil {
		t.Fatalf("Put: %v", err)
	}
	loaded, err := s.Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}

	if string(loaded.Envelope.Policy) != string(raw) {
		t.Errorf("the cached policy bytes changed:\n got %s\nwant %s", loaded.Envelope.Policy, raw)
	}

	// The point of the assertion above, stated as the reader experiences it.
	policy, err := loaded.Envelope.Verify(productclient.VerifyOptions{
		TrustedKeys: map[string]string{testKeyID: pub},
		Audience:    testAudience,
		Now:         time.Now(),
	})
	if err != nil {
		t.Fatalf("a catalog read back from the cache does not verify: %v", err)
	}
	if policy.Catalog.Version != testCatalogVersion {
		t.Errorf("catalog version = %q, want %q", policy.Catalog.Version, testCatalogVersion)
	}
	if policy.Catalog.Models[0].DisplayName.En != `Tools & Agents <beta> "x"\y` {
		t.Errorf("display name did not survive the round trip: %q", policy.Catalog.Models[0].DisplayName.En)
	}
}

// TestPutRefusesAPolicyWhoseBytesWouldNotSurvive covers the case the escaping
// fix cannot: a policy the platform wrote with insignificant whitespace cannot
// be stored as json.RawMessage, because the JSON encoder compacts it and the
// signature then covers bytes nobody can reproduce.
//
// Refusing is the point. Writing it anyway would produce a file that verifies
// never, forever, with the cause several layers away from the symptom.
func TestPutRefusesAPolicyWhoseBytesWouldNotSurvive(t *testing.T) {
	s, root := openStore(t)
	priv, _ := signingPair(t)
	pretty := []byte("{\n  \"policyVersion\": \"2026-09-12.1\",\n  \"catalog\": {}\n}")

	err := s.Put(entry(t, priv, pretty, testCatalogVersion))
	if !errors.Is(err, catalogstore.ErrNotByteExact) {
		t.Fatalf("Put error = %v, want ErrNotByteExact", err)
	}
	if _, statErr := os.Stat(cachePath(root)); !errors.Is(statErr, os.ErrNotExist) {
		t.Errorf("a cache was written anyway (%v); it would never verify", statErr)
	}
}

// TestANewerCatalogReplacesTheCache is the ordinary path.
func TestANewerCatalogReplacesTheCache(t *testing.T) {
	s, _ := openStore(t)
	priv, _ := signingPair(t)

	if err := s.Put(entry(t, priv, policyBytes(t, "2026-09-12.1"), "2026-09-12.1")); err != nil {
		t.Fatalf("Put v1: %v", err)
	}
	if err := s.Put(entry(t, priv, policyBytes(t, "2026-09-13.1"), "2026-09-13.1")); err != nil {
		t.Fatalf("Put v2: %v", err)
	}
	loaded, err := s.Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if loaded.CatalogVersion != "2026-09-13.1" {
		t.Errorf("cached version = %q, want the newer one", loaded.CatalogVersion)
	}
}

// TestARolledBackCatalogIsRefusedAndTheCacheIsUntouched is the downgrade attack
// (中台交付包 §4.3 末表): a validly signed but older catalog must not be able to
// replace a newer one. Asserting on the file bytes rather than on the return
// value alone matters - a refusal that still rewrites the cache is not a
// refusal.
func TestARolledBackCatalogIsRefusedAndTheCacheIsUntouched(t *testing.T) {
	s, root := openStore(t)
	priv, _ := signingPair(t)

	if err := s.Put(entry(t, priv, policyBytes(t, "2026-09-12.1"), "2026-09-12.1")); err != nil {
		t.Fatalf("Put v1: %v", err)
	}
	before := readRaw(t, cachePath(root))

	err := s.Put(entry(t, priv, policyBytes(t, "2026-09-01.1"), "2026-09-01.1"))
	if !errors.Is(err, catalogstore.ErrRolledBack) {
		t.Fatalf("Put error = %v, want ErrRolledBack", err)
	}
	if string(readRaw(t, cachePath(root))) != string(before) {
		t.Error("the cache changed while refusing a rolled-back catalog")
	}
	loaded, _ := s.Load()
	if loaded.CatalogVersion != "2026-09-12.1" {
		t.Errorf("cached version = %q, want the newer one kept", loaded.CatalogVersion)
	}
}

// TestAnEqualVersionIsNotRewritten is 需求基线 B2 规则 4: write only when the
// version advances. A login that fetches the same catalog must not touch the
// drive - on a USB stick that is the difference between a read and a write on
// every sign-in.
func TestAnEqualVersionIsNotRewritten(t *testing.T) {
	s, root := openStore(t)
	priv, _ := signingPair(t)
	raw := policyBytes(t, testCatalogVersion)

	if err := s.Put(entry(t, priv, raw, testCatalogVersion)); err != nil {
		t.Fatalf("Put: %v", err)
	}
	before := readRaw(t, cachePath(root))

	// A second entry for the same catalog version, but stamped differently: if
	// the store rewrites, fetchedAt in the file will move.
	second := entry(t, priv, raw, testCatalogVersion)
	second.FetchedAt = second.FetchedAt.Add(90 * time.Minute)
	if err := s.Put(second); err != nil {
		t.Fatalf("Put (same version): %v", err)
	}
	if string(readRaw(t, cachePath(root))) != string(before) {
		t.Error("the cache was rewritten for an unchanged catalog version")
	}
}

// TestCorruptFileIsReportedAndPreserved covers 需求基线 E6.2 规则 3/5 applied to
// this file: a parse failure must not be able to destroy what is on disk, and it
// must be reported rather than passing for a fresh install.
func TestCorruptFileIsReportedAndPreserved(t *testing.T) {
	garbage := "{ this is not json"
	s, root := openStoreSeeded(t, garbage)

	if _, err := s.Load(); !errors.Is(err, catalogstore.ErrCorrupt) {
		t.Fatalf("Load error = %v, want ErrCorrupt", err)
	}
	if string(readRaw(t, cachePath(root))) != string(garbage) {
		t.Error("the corrupt file was modified; a parse bug must not be able to delete the user's data")
	}

	// A corrupt cache carries no version, so it cannot be a baseline. The next
	// verified catalog replaces it - the alternative is a user locked out of
	// their own cache with no way back (开发计划 PR-4b 第 2 步补充 ④).
	priv, _ := signingPair(t)
	if err := s.Put(entry(t, priv, policyBytes(t, testCatalogVersion), testCatalogVersion)); err != nil {
		t.Fatalf("Put over a corrupt cache: %v", err)
	}
	if _, err := s.Load(); err != nil {
		t.Fatalf("Load after replacing a corrupt cache: %v", err)
	}
}

// TestANewerSchemaVersionIsRefused is 需求基线 E6.2 规则 4 on this file: a cache
// written by a newer build must not be read by an older one.
//
// It is refused rather than upgraded, and the file is left alone - the same
// treatment as a corrupt one. Replacing it later with a freshly verified catalog
// does not violate rule 4, because nothing is re-encoded from the new file: the
// write is a whole new entry, so there are no unknown fields to drop silently.
func TestANewerSchemaVersionIsRefused(t *testing.T) {
	body := `{"schemaVersion":` + itoa(catalogstore.CurrentSchemaVersion+1) + `,"catalogVersion":"` + testCatalogVersion + `"}`
	s, root := openStoreSeeded(t, body)

	if _, err := s.Load(); !errors.Is(err, catalogstore.ErrIncompatibleVersion) {
		t.Fatalf("Load error = %v, want ErrIncompatibleVersion", err)
	}
	if string(readRaw(t, cachePath(root))) != body {
		t.Error("the newer-version file was modified; refusing to read it must not also rewrite it")
	}
}

func itoa(n int) string { return strconv.Itoa(n) }

// TestVersionOrderingIsNotStringOrdering is the nail under a defect this PR's
// own testing found: the store orders catalogs to decide what counts as a
// rollback, and a string compare is not that ordering.
//
// The documented version is a date with a build counter (中台交付包 §4.3:
// `2026-09-10.1`). String order agrees with it only while every part is one
// character wide - and it stops agreeing the first time a day carries a tenth
// catalog, because "…-09.10" sorts *below* "…-09.9". A store that compared
// strings would read the tenth catalog of a day as a rollback and refuse every
// refresh until midnight, with nothing in any log to explain it.
//
// Asserted through Put/Load rather than against the comparator, because the
// user-visible fact is "the newer catalog is accepted", not "a helper returns
// the right sign".
func TestVersionOrderingIsNotStringOrdering(t *testing.T) {
	s, root := openStore(t)
	priv, _ := signingPair(t)

	if err := s.Put(entry(t, priv, policyBytes(t, "2026-09-09.9"), "2026-09-09.9")); err != nil {
		t.Fatalf("Put .9: %v", err)
	}

	// String order would refuse this one.
	if err := s.Put(entry(t, priv, policyBytes(t, "2026-09-09.10"), "2026-09-09.10")); err != nil {
		t.Fatalf("Put .10 after .9: %v (string order would call the newer catalog a rollback)", err)
	}
	loaded, err := s.Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if loaded.CatalogVersion != "2026-09-09.10" {
		t.Errorf("cached version = %q, want the newer .10", loaded.CatalogVersion)
	}

	// And the reverse still is a rollback, or the fix would have disabled the
	// defence instead of correcting it.
	if err := s.Put(entry(t, priv, policyBytes(t, "2026-09-09.9"), "2026-09-09.9")); !errors.Is(err, catalogstore.ErrRolledBack) {
		t.Errorf("Put .9 over .10 = %v, want ErrRolledBack", err)
	}

	// Unpadded parts must name the same version, not an older one: the parse
	// normalises them, so the store does not depend on zero padding the contract
	// never promised. Asserted through Put - an "equal" version must leave the
	// file alone, which is what the padded spelling would also do.
	before := readRaw(t, cachePath(root))
	second := entry(t, priv, policyBytes(t, "2026-09-09.10"), "2026-9-9.10")
	second.FetchedAt = second.FetchedAt.Add(30 * time.Minute)
	if err := s.Put(second); err != nil {
		t.Fatalf("Put of the unpadded spelling: %v", err)
	}
	if string(readRaw(t, cachePath(root))) != string(before) {
		t.Error("`2026-9-9.10` was not recognised as the same version as `2026-09-09.10`")
	}
}

// TestTheCacheHoldsNoCredential is the same nail product-state.json carries
// (需求基线 E6 规则 3): the cache travels on a USB stick, so it must be safe to
// copy. The envelope is published data, and the metadata around it is public.
func TestTheCacheHoldsNoCredential(t *testing.T) {
	s, root := openStore(t)
	priv, _ := signingPair(t)
	if err := s.Put(entry(t, priv, policyBytes(t, testCatalogVersion), testCatalogVersion)); err != nil {
		t.Fatalf("Put: %v", err)
	}

	body := strings.ToLower(string(readRaw(t, cachePath(root))))
	for _, needle := range []string{"accesstoken", "refreshtoken", "access_token", "refresh_token", "authorization"} {
		if strings.Contains(body, needle) {
			t.Errorf("catalog.json contains %q; the cache must never hold a credential", needle)
		}
	}
}

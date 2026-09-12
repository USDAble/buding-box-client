// Package catalogstore owns data/catalog.json, the cached signed catalog.
//
// It is homologous to internal/productstate and internal/credentialstore: one
// file, one owner, every path resolved through internal/datapath. The cache
// travels on a USB stick, so it holds no credential and no personal data - and
// it is the only reason the picker works offline, which is why the signed
// envelope is stored verbatim rather than projected into local structs
// (需求基线 B2 规则 1).
//
// What this package does NOT do is verify signatures. Verification belongs to
// internal/productclient, and keeping it there is what stops the trust anchor
// from growing a second implementation. The store refuses a catalog it cannot
// keep faithfully; it does not judge whether the catalog is genuine.
package catalogstore

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/open-octo/octo-agent/internal/atomicfile"
	"github.com/open-octo/octo-agent/internal/datapath"
	"github.com/open-octo/octo-agent/internal/productclient"
)

// CurrentSchemaVersion is the structure version this build writes. It only ever
// increases (需求基线 E6.2 规则 1).
const CurrentSchemaVersion = 1

// cacheFile is the file name under the data root.
const cacheFile = "catalog.json"

var (
	// ErrNoCache means there is no catalog yet. Absence is not damage: a fresh
	// installation is in this state (E6.2 规则 2).
	ErrNoCache = errors.New("catalogstore: no cached catalog")
	// ErrCorrupt means the file exists but could not be parsed. The file is left
	// untouched on disk (E6.2 规则 5).
	ErrCorrupt = errors.New("catalogstore: cached catalog is corrupt")
	// ErrIncompatibleVersion means the file was written by a newer build
	// (E6.2 规则 4).
	ErrIncompatibleVersion = errors.New("catalogstore: cached catalog is from a newer version")
	// ErrRolledBack means the catalog offered is older than the one cached. It is
	// the downgrade defence (需求基线 B2 规则 3): a validly signed old catalog is
	// exactly what a replaying attacker has, so the cache is where it is caught.
	ErrRolledBack = errors.New("catalogstore: catalog version is older than the cached one")
	// ErrNotByteExact means the policy bytes could not be stored verbatim, so a
	// signature over them would never verify again. Refusing is deliberate: the
	// alternative is a cache that fails forever with the cause several layers
	// away from the symptom.
	ErrNotByteExact = errors.New("catalogstore: policy bytes would not survive a round trip")
)

// Options configure Open.
type Options struct {
	// Now overrides the clock, for tests.
	Now func() time.Time
}

// Entry is one cached catalog: the signed envelope plus the metadata the
// platform sent with it (需求基线 B2 规则 1).
type Entry struct {
	CatalogVersion string
	FetchedAt      time.Time
	ExpiresAt      time.Time
	KeyID          string
	Audience       string
	Envelope       productclient.PolicyEnvelope
}

// diskEntry is the file shape. It is separate from Entry so the on-disk spellings
// live in one place and the API stays in Go types.
type diskEntry struct {
	SchemaVersion  int                          `json:"schemaVersion"`
	CatalogVersion string                       `json:"catalogVersion"`
	FetchedAt      string                       `json:"fetchedAt"`
	ExpiresAt      string                       `json:"expiresAt"`
	KeyID          string                       `json:"keyId"`
	Audience       string                       `json:"audience"`
	Envelope       productclient.PolicyEnvelope `json:"envelope"`
}

// Store is the in-memory view of the cache file. It is safe for concurrent use.
type Store struct {
	mu      sync.Mutex
	path    string
	current Entry // zero when there is no usable cache
	corrupt bool
	// lastErr records why the file could not be loaded, so Load can report a
	// parse failure distinctly from absence (E6.2 规则 5).
	lastErr error

	now func() time.Time
}

// Open loads the cache. It never writes: absence means "no cache yet", not
// "materialise an empty one" (开发规范 §3.9.1). A file that cannot be read is
// reported through Load rather than here, so a caller that only wants to serve
// a request is not forced to handle a storage failure it does not need yet.
func Open(opts Options) (*Store, error) {
	path, err := datapath.Join(cacheFile)
	if err != nil {
		return nil, fmt.Errorf("catalogstore: resolve path: %w", err)
	}
	now := opts.Now
	if now == nil {
		now = time.Now
	}
	s := &Store{path: path, now: now}
	s.load()
	return s, nil
}

// Load returns the cached entry, or a sentinel describing why there is none.
func (s *Store) Load() (Entry, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.lastErr != nil {
		return Entry{}, s.lastErr
	}
	if s.current.CatalogVersion == "" {
		return Entry{}, ErrNoCache
	}
	return s.current, nil
}

// Put stores a catalog.
//
// The write happens only when the version advances (需求基线 B2 规则 4): on a USB
// stick that is the difference between a read and a write on every sign-in. An
// equal version is a no-op, and an older one is refused - neither is an error the
// caller has to act on, which is why the caller compares outcomes rather than
// checking for these two before calling.
func (s *Store) Put(entry Entry) error {
	if entry.CatalogVersion == "" {
		return fmt.Errorf("%w: entry carries no catalog version", ErrNotByteExact)
	}
	// Byte-exactness is checked before anything touches the disk, because a
	// half-written cache is worse than a refusal (see ErrNotByteExact).
	raw, err := encode(entry)
	if err != nil {
		return err
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	// A corrupt file carries no version, so it cannot be a baseline: replacing it
	// is the only way back for a user whose cache was damaged (开发计划 PR-4b
	// 第 2 步补充 ④). The corruption is not a reason to prefer an attacker's
	// replay - there is simply nothing to compare against.
	// The comparison below is a version ordering, not a string ordering, and the
	// distinction is not academic: see compareCatalogVersions.
	switch {
	case s.lastErr == nil && compareCatalogVersions(entry.CatalogVersion, s.current.CatalogVersion) == 0:
		s.clearError()
		return nil
	case s.lastErr == nil && compareCatalogVersions(entry.CatalogVersion, s.current.CatalogVersion) < 0:
		return fmt.Errorf("%w: cached %q, offered %q", ErrRolledBack, s.current.CatalogVersion, entry.CatalogVersion)
	}

	if err := atomicfile.WriteFile(s.path, raw, 0o600); err != nil {
		return err
	}
	s.current = entry
	s.clearError()
	return nil
}

// Path is the file this store owns, for diagnostics and tests.
func (s *Store) Path() string { return s.path }

// Corrupt reports whether the file on disk was unparseable and preserved.
func (s *Store) Corrupt() bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.corrupt
}

func (s *Store) clearError() {
	s.corrupt = false
	s.lastErr = nil
}

// load reads and parses the file. The caller holds no lock only from Open; from
// then on the in-memory copy is authoritative and the file is not re-read.
func (s *Store) load() {
	raw, err := os.ReadFile(s.path)
	if errors.Is(err, os.ErrNotExist) {
		return
	}
	if err != nil {
		s.lastErr = err
		return
	}

	var disk diskEntry
	if err := json.Unmarshal(raw, &disk); err != nil {
		s.corrupt = true
		s.lastErr = fmt.Errorf("%w: %v", ErrCorrupt, err)
		return
	}
	if disk.SchemaVersion > CurrentSchemaVersion {
		s.lastErr = fmt.Errorf("%w: file has %d, this build understands %d",
			ErrIncompatibleVersion, disk.SchemaVersion, CurrentSchemaVersion)
		return
	}
	fetchedAt, err := time.Parse(time.RFC3339, disk.FetchedAt)
	if err != nil {
		s.corrupt = true
		s.lastErr = fmt.Errorf("%w: fetchedAt %q: %v", ErrCorrupt, disk.FetchedAt, err)
		return
	}
	expiresAt, err := time.Parse(time.RFC3339, disk.ExpiresAt)
	if err != nil {
		s.corrupt = true
		s.lastErr = fmt.Errorf("%w: expiresAt %q: %v", ErrCorrupt, disk.ExpiresAt, err)
		return
	}
	s.current = Entry{
		CatalogVersion: disk.CatalogVersion,
		FetchedAt:      fetchedAt,
		ExpiresAt:      expiresAt,
		KeyID:          disk.KeyID,
		Audience:       disk.Audience,
		Envelope:       disk.Envelope,
	}
}

// encode renders the entry as the bytes that go on disk, then reads them back
// and refuses if the policy did not survive.
//
// The round trip is the point. `Policy` is json.RawMessage, and encoding/json
// rewrites it on the way out - it compacts insignificant whitespace, and by
// default it also escapes `<`, `>`, `&` into `\u003c`-style sequences. A policy
// containing either is rewritten, and the signature over it stops verifying.
// Checking here means the failure surfaces as a refusal at fetch time rather
// than as a cache that verifies never, forever, with the cause several layers
// away from the symptom (开发计划 PR-4b 第 2 步补充 ①).
func encode(entry Entry) ([]byte, error) {
	disk := diskEntry{
		SchemaVersion:  CurrentSchemaVersion,
		CatalogVersion: entry.CatalogVersion,
		FetchedAt:      entry.FetchedAt.UTC().Format(time.RFC3339),
		ExpiresAt:      entry.ExpiresAt.UTC().Format(time.RFC3339),
		KeyID:          entry.KeyID,
		Audience:       entry.Audience,
		Envelope:       entry.Envelope,
	}
	buf, err := marshalCache(disk)
	if err != nil {
		return nil, err
	}
	buf = append(buf, '\n')

	// Self-check: the policy must come back byte-for-byte.
	var back diskEntry
	if err := json.Unmarshal(buf, &back); err != nil {
		return nil, fmt.Errorf("%w: the encoded cache does not parse: %v", ErrNotByteExact, err)
	}
	if !bytes.Equal(back.Envelope.Policy, entry.Envelope.Policy) {
		return nil, fmt.Errorf("%w: the JSON encoder rewrote the policy (a signature over it would never verify)", ErrNotByteExact)
	}
	return buf, nil
}

// catalogVersionShape is the documented form of a catalog version:
// `YYYY-MM-DD` with an optional `.<counter>` build suffix (中台交付包 §4.3
// example: `2026-09-10.1`).
var catalogVersionShape = regexp.MustCompile(`^(\d{4})-(\d{1,2})-(\d{1,2})(?:\.(\d+))?$`)

type catalogVersion struct{ year, month, day, counter int }

func parseCatalogVersion(s string) (catalogVersion, bool) {
	m := catalogVersionShape.FindStringSubmatch(s)
	if m == nil {
		return catalogVersion{}, false
	}
	var v catalogVersion
	fields := []*int{&v.year, &v.month, &v.day}
	for i, dst := range fields {
		n, err := strconv.Atoi(m[i+1])
		if err != nil {
			return catalogVersion{}, false
		}
		*dst = n
	}
	if m[4] != "" {
		n, err := strconv.Atoi(m[4])
		if err != nil {
			return catalogVersion{}, false
		}
		v.counter = n
	}
	return v, true
}

// compareCatalogVersions orders two catalog versions: negative when a is older,
// zero when equal.
//
// It is not a string compare, and the difference is a real defect rather than a
// hypothetical one. String order only agrees with version order while every
// part of the version happens to be one character wide, and the documented shape
// - a date with a build counter - breaks that the first time a day carries a
// tenth catalog: "2026-09-09.10" sorts *below* "2026-09-09.9". A store that
// string-compares would then treat the tenth catalog of a day as a rollback and
// refuse every refresh for the rest of that day, silently, with nothing in any
// log to say why.
//
// So the parts are parsed and compared as numbers. A version that does not match
// the documented shape falls back to a string compare: that cannot invent an
// ordering, and refusing to compare at all would let an unparseable version
// bypass the downgrade defence entirely.
func compareCatalogVersions(a, b string) int {
	av, aok := parseCatalogVersion(a)
	bv, bok := parseCatalogVersion(b)
	if !aok || !bok {
		return strings.Compare(a, b)
	}
	for _, d := range []int{av.year - bv.year, av.month - bv.month, av.day - bv.day, av.counter - bv.counter} {
		if d != 0 {
			return d
		}
	}
	return 0
}

// marshalCache serialises the cache with HTML escaping off, which is the one
// setting that decides whether a policy containing `&` survives the write.
//
// There is deliberately no indentation. json.Indent rewrites the whole document,
// including the inside of the policy - so the one readable courtesy this file
// could have would break the one property it must have. A cache is read by a
// program; the copy a human inspects is the envelope in a diagnostic bundle.
func marshalCache(disk diskEntry) ([]byte, error) {
	var buf bytes.Buffer
	enc := json.NewEncoder(&buf)
	enc.SetEscapeHTML(false)
	if err := enc.Encode(disk); err != nil {
		return nil, fmt.Errorf("catalogstore: encode: %w", err)
	}
	// Encode appends a newline; the caller adds the file's own so the two are
	// not confused.
	return bytes.TrimRight(buf.Bytes(), "\n"), nil
}

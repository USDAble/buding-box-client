// OCTO-FORK: L-D5 server dictionary cache — see the product baseline D4/D5.
package sensitive

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"math/big"
	"os"
	"strings"
	"sync"
	"time"

	"github.com/open-octo/octo-agent/internal/atomicfile"
	"github.com/open-octo/octo-agent/internal/datapath"
)

// ServerDictFileName is the single owner of the read-only server dictionary's
// location inside the data root (需求基线 D4).
const ServerDictFileName = "sensitive-dict-server.json"

var (
	ErrNoServerDictionary         = errors.New("sensitive: no cached server dictionary")
	ErrServerDictionaryCorrupt    = errors.New("sensitive: cached server dictionary is corrupt")
	ErrServerDictionaryRolledBack = errors.New("sensitive: server dictionary version rolled back")
	ErrInvalidServerDictionary    = errors.New("sensitive: invalid server dictionary")
)

// ServerEntry is the validated snapshot stored in data/sensitive-dict-server.json.
// Signature verification happens before Put; this store owns persistence and
// monotonicity, not trust-anchor policy.
type ServerEntry struct {
	Version   string
	KeyID     string
	Signature string
	Words     []string
	FetchedAt time.Time
}

type serverDiskEntry struct {
	Version   string   `json:"version"`
	KeyID     string   `json:"keyId"`
	Signature string   `json:"signature"`
	Words     []string `json:"words"`
	FetchedAt string   `json:"fetchedAt"`
}

// ServerStore owns the last accepted server dictionary. Open is read-only;
// only Put after a successful sync creates or replaces the cache.
type ServerStore struct {
	mu      sync.Mutex
	path    string
	current ServerEntry
	lastErr error
}

func OpenServerStore() (*ServerStore, error) {
	path, err := datapath.Join(ServerDictFileName)
	if err != nil {
		return nil, fmt.Errorf("sensitive: resolve server dictionary path: %w", err)
	}
	s := &ServerStore{path: path}
	s.load()
	return s, nil
}

// NewFromDataRoot builds the product engine from the two cache owners under the
// configured data root. The server file name and user file name remain owned by
// this package; callers neither spell nor resolve either path themselves.
func NewFromDataRoot(server *ServerStore) (*Engine, error) {
	userPath, err := datapath.Join(DictFileName)
	if err != nil {
		return nil, fmt.Errorf("sensitive: resolve user dictionary path: %w", err)
	}
	return NewWithServer(userPath, server), nil
}

// Load returns a defensive copy so the engine cannot mutate the store's owner.
func (s *ServerStore) Load() (ServerEntry, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.lastErr != nil {
		return ServerEntry{}, s.lastErr
	}
	if s.current.Version == "" {
		return ServerEntry{}, ErrNoServerDictionary
	}
	return cloneServerEntry(s.current), nil
}

// Put atomically stores a verified full snapshot. Equal versions are a no-op;
// an older version is refused without touching the last valid bytes.
func (s *ServerStore) Put(entry ServerEntry) error {
	if err := validateServerEntry(entry); err != nil {
		return err
	}
	raw, err := encodeServerEntry(entry)
	if err != nil {
		return err
	}

	s.mu.Lock()
	defer s.mu.Unlock()
	if s.lastErr == nil && s.current.Version != "" {
		cmp, err := compareServerVersions(entry.Version, s.current.Version)
		if err != nil {
			return err
		}
		if cmp == 0 {
			return nil
		}
		if cmp < 0 {
			return fmt.Errorf("%w: cached %q, offered %q", ErrServerDictionaryRolledBack, s.current.Version, entry.Version)
		}
	}
	if err := atomicfile.WriteFile(s.path, raw, 0o600); err != nil {
		return err
	}
	s.current = cloneServerEntry(entry)
	s.lastErr = nil
	return nil
}

func (s *ServerStore) load() {
	raw, err := os.ReadFile(s.path)
	if errors.Is(err, os.ErrNotExist) {
		return
	}
	if err != nil {
		s.lastErr = err
		return
	}
	var disk serverDiskEntry
	if err := json.Unmarshal(raw, &disk); err != nil {
		s.lastErr = fmt.Errorf("%w: %v", ErrServerDictionaryCorrupt, err)
		return
	}
	fetchedAt, err := time.Parse(time.RFC3339, disk.FetchedAt)
	if err != nil {
		s.lastErr = fmt.Errorf("%w: fetchedAt %q", ErrServerDictionaryCorrupt, disk.FetchedAt)
		return
	}
	entry := ServerEntry{
		Version: disk.Version, KeyID: disk.KeyID, Signature: disk.Signature,
		Words: disk.Words, FetchedAt: fetchedAt,
	}
	if err := validateServerEntry(entry); err != nil {
		s.lastErr = fmt.Errorf("%w: %v", ErrServerDictionaryCorrupt, err)
		return
	}
	s.current = cloneServerEntry(entry)
}

func validateServerEntry(entry ServerEntry) error {
	if _, err := parseServerVersion(entry.Version); err != nil {
		return err
	}
	if strings.TrimSpace(entry.KeyID) == "" || strings.TrimSpace(entry.Signature) == "" || entry.FetchedAt.IsZero() {
		return fmt.Errorf("%w: missing keyId, signature or fetchedAt", ErrInvalidServerDictionary)
	}
	for _, word := range entry.Words {
		if strings.ContainsAny(word, "\r\n") {
			return fmt.Errorf("%w: word contains a line break", ErrInvalidServerDictionary)
		}
		if _, ok := NormalizeWord(word); !ok {
			return fmt.Errorf("%w: unusable word", ErrInvalidServerDictionary)
		}
	}
	return nil
}

func encodeServerEntry(entry ServerEntry) ([]byte, error) {
	words := append([]string(nil), entry.Words...)
	if words == nil {
		words = []string{}
	}
	var buf bytes.Buffer
	enc := json.NewEncoder(&buf)
	enc.SetEscapeHTML(false)
	if err := enc.Encode(serverDiskEntry{
		Version: entry.Version, KeyID: entry.KeyID, Signature: entry.Signature,
		Words: words, FetchedAt: entry.FetchedAt.UTC().Format(time.RFC3339),
	}); err != nil {
		return nil, fmt.Errorf("sensitive: encode server dictionary: %w", err)
	}
	return buf.Bytes(), nil
}

func compareServerVersions(a, b string) (int, error) {
	av, err := parseServerVersion(a)
	if err != nil {
		return 0, err
	}
	bv, err := parseServerVersion(b)
	if err != nil {
		return 0, err
	}
	return av.Cmp(bv), nil
}

func parseServerVersion(version string) (*big.Int, error) {
	if version == "" || strings.TrimSpace(version) != version || strings.HasPrefix(version, "+") || strings.HasPrefix(version, "-") {
		return nil, fmt.Errorf("%w: version %q is not an unsigned integer", ErrInvalidServerDictionary, version)
	}
	n, ok := new(big.Int).SetString(version, 10)
	if !ok {
		return nil, fmt.Errorf("%w: version %q is not an unsigned integer", ErrInvalidServerDictionary, version)
	}
	return n, nil
}

func cloneServerEntry(entry ServerEntry) ServerEntry {
	entry.Words = append([]string(nil), entry.Words...)
	return entry
}

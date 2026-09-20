package productruntime

import (
	"context"
	"encoding/json"
	"fmt"
	"github.com/open-octo/octo-agent/internal/productclient"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"
)

type capabilitySnapshot struct {
	data    json.RawMessage
	updated time.Time
}
type capabilityFlight struct {
	done     chan struct{}
	err      error
	snapshot capabilitySnapshot
}
type capabilityCache struct {
	mu       sync.Mutex
	scope    string
	entries  map[string]capabilitySnapshot
	fetching map[string]*capabilityFlight
}

const capabilityDetailTTL = time.Minute

// store is called under mu. The bounded cache cannot grow with arbitrary IDs.
func (cache *capabilityCache) store(key string, snapshot capabilitySnapshot) {
	if len(cache.entries) >= 128 {
		var oldestKey string
		var oldest time.Time
		for k, v := range cache.entries {
			if oldestKey == "" || v.updated.Before(oldest) {
				oldestKey = k
				oldest = v.updated
			}
		}
		delete(cache.entries, oldestKey)
	}
	cache.entries[key] = snapshot
}

func (rt *Runtime) handleExperts(w http.ResponseWriter, r *http.Request) {
	rt.directory(w, r, "experts")
}
func (rt *Runtime) handlePlatformSkills(w http.ResponseWriter, r *http.Request) {
	rt.directory(w, r, "skills")
}

// Cached metadata renders immediately. A refresh reports its actual failure;
// cached content never masquerades as a freshly verified publication.
func (rt *Runtime) directory(w http.ResponseWriter, r *http.Request, kind string) {
	if !rt.financeReady(w) {
		return
	}
	if err := rt.deps.Platform.EnsureToken(r.Context()); err != nil {
		rt.failPlatform(w, err)
		return
	}
	scope := rt.deps.Platform.SessionCacheScope()
	cache := &rt.capabilities
	for {
		if rt.deps.Platform.SessionCacheScope() != scope || !rt.deps.State.State().LoggedIn {
			writeCode(w, 409, "directory_scope_changed", nil)
			return
		}
		cache.mu.Lock()
		if cache.scope != scope {
			cache.scope = scope
			cache.entries = map[string]capabilitySnapshot{}
			cache.fetching = map[string]*capabilityFlight{}
		}
		snapshot, ok := cache.entries[kind]
		if ok && r.URL.Query().Get("refresh") != "1" {
			cache.mu.Unlock()
			rt.writeDirectory(w, snapshot, true)
			return
		}
		if pending := cache.fetching[kind]; pending != nil {
			cache.mu.Unlock()
			select {
			case <-pending.done:
				if rt.deps.Platform.SessionCacheScope() != scope || !rt.deps.State.State().LoggedIn {
					writeCode(w, 409, "directory_scope_changed", nil)
					return
				}
				if pending.err != nil {
					rt.failPlatform(w, pending.err)
					return
				}
				rt.writeDirectory(w, pending.snapshot, false)
				return
			case <-r.Context().Done():
				return
			}
			continue
		}
		pending := &capabilityFlight{done: make(chan struct{})}
		cache.fetching[kind] = pending
		cache.mu.Unlock()
		var out any
		var err error
		if kind == "experts" {
			out, err = rt.deps.Platform.Experts(r.Context())
		} else {
			out, err = rt.deps.Platform.Skills(r.Context())
		}
		var raw []byte
		if err == nil {
			raw, err = json.Marshal(out)
		}
		snapshot = capabilitySnapshot{data: raw, updated: time.Now().UTC()}
		cache.mu.Lock()
		same := cache.scope == scope && rt.deps.Platform.SessionCacheScope() == scope && rt.deps.State.State().LoggedIn
		if cache.scope == scope {
			delete(cache.fetching, kind)
			if err == nil && same {
				cache.store(kind, snapshot)
			}
		}
		pending.err = err
		pending.snapshot = snapshot
		close(pending.done)
		cache.mu.Unlock()
		if !same {
			writeCode(w, http.StatusConflict, "directory_scope_changed", nil)
			return
		}
		if err != nil {
			rt.failPlatform(w, err)
			return
		}
		rt.writeDirectory(w, snapshot, false)
		return
	}
}
func (rt *Runtime) writeDirectory(w http.ResponseWriter, snapshot capabilitySnapshot, cached bool) {
	var body map[string]any
	if json.Unmarshal(snapshot.data, &body) != nil {
		writeCode(w, 503, "directory_cache_invalid", nil)
		return
	}
	body["cached"] = cached
	body["updatedAt"] = snapshot.updated.Format(time.RFC3339)
	writeJSON(w, 200, body)
}
func (rt *Runtime) handleExpertDetail(w http.ResponseWriter, r *http.Request) {
	rt.capabilityDetail(w, r, true)
}
func (rt *Runtime) handleSkillDetail(w http.ResponseWriter, r *http.Request) {
	rt.capabilityDetail(w, r, false)
}
func (rt *Runtime) capabilityDetail(w http.ResponseWriter, r *http.Request, expert bool) {
	if !rt.financeReady(w) {
		return
	}
	version, err := strconv.ParseUint(r.URL.Query().Get("version"), 10, 32)
	if err != nil || version == 0 {
		writeCode(w, 400, "invalid_version", nil)
		return
	}
	out, err := rt.cachedCapabilityDetail(r.Context(), expert, r.PathValue("id"), uint32(version))
	if err != nil {
		rt.failPlatform(w, err)
		return
	}
	writeJSON(w, 200, out)
}

func (rt *Runtime) cachedCapabilityDetail(ctx context.Context, expert bool, id string, version uint32) (json.RawMessage, error) {
	if rt.deps.Platform == nil || !rt.deps.State.State().LoggedIn {
		return nil, fmt.Errorf("platform session is unavailable")
	}
	if err := rt.deps.Platform.EnsureToken(ctx); err != nil {
		return nil, err
	}
	scope := rt.deps.Platform.SessionCacheScope()
	key := fmt.Sprintf("detail:%t:%s:%d", expert, id, version)
	cache := &rt.capabilities
	validScope := func() bool { return rt.deps.State.State().LoggedIn && rt.deps.Platform.SessionCacheScope() == scope }
	cache.mu.Lock()
	if cache.scope != scope {
		cache.scope = scope
		cache.entries = map[string]capabilitySnapshot{}
		cache.fetching = map[string]*capabilityFlight{}
	}
	if entry, ok := cache.entries[key]; ok && time.Since(entry.updated) < capabilityDetailTTL {
		cache.mu.Unlock()
		if !validScope() {
			return nil, fmt.Errorf("platform account changed")
		}
		return entry.data, nil
	}
	if pending := cache.fetching[key]; pending != nil {
		cache.mu.Unlock()
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-pending.done:
		}
		if !validScope() {
			return nil, fmt.Errorf("platform account changed")
		}
		return pending.snapshot.data, pending.err
	}
	pending := &capabilityFlight{done: make(chan struct{})}
	cache.fetching[key] = pending
	cache.mu.Unlock()
	var out any
	var err error
	if expert {
		var item *productclient.PlatformExpert
		item, err = rt.deps.Platform.ExpertDetail(ctx, id, version)
		out = item
		if err == nil && (item.ID != id || item.Version != version) {
			err = fmt.Errorf("platform expert publication mismatch")
		}
	} else {
		var item *productclient.PlatformSkill
		item, err = rt.deps.Platform.SkillDetail(ctx, id, version)
		out = item
		if err == nil && (item.ID != id || item.Version != version) {
			err = fmt.Errorf("platform skill publication mismatch")
		}
	}
	var raw []byte
	if err == nil {
		raw, err = json.Marshal(out)
	}
	if !validScope() {
		err = fmt.Errorf("platform account changed during detail loading")
	}
	snapshot := capabilitySnapshot{data: raw, updated: time.Now().UTC()}
	cache.mu.Lock()
	if cache.scope == scope {
		delete(cache.fetching, key)
		if err == nil {
			cache.store(key, snapshot)
		}
	}
	pending.err = err
	pending.snapshot = snapshot
	close(pending.done)
	cache.mu.Unlock()
	return raw, err
}

// LoadPlatformSkill reads only the selected expert publication's bound skill.
// Its body enters the real tool result; failures never select another expert.
func (rt *Runtime) LoadPlatformSkill(ctx context.Context, expertID string, version uint32, skillID string) (string, error) {
	if rt.deps.Platform == nil || !rt.deps.State.State().LoggedIn {
		return "", fmt.Errorf("platform session is unavailable")
	}
	raw, err := rt.cachedCapabilityDetail(ctx, true, expertID, version)
	if err != nil {
		return "", err
	}
	var expert productclient.PlatformExpert
	if err = json.Unmarshal(raw, &expert); err != nil {
		return "", err
	}
	if expert.ID != expertID || expert.Version != version {
		return "", fmt.Errorf("platform expert publication mismatch")
	}
	for _, skill := range expert.Skills {
		if skill.ID == skillID {
			if strings.TrimSpace(skill.Content) == "" {
				return "", fmt.Errorf("platform skill content unavailable")
			}
			return fmt.Sprintf("%s (v%d)\n\n%s", skill.Name, skill.Version, skill.Content), nil
		}
	}
	return "", fmt.Errorf("skill is not bound to this expert publication")
}

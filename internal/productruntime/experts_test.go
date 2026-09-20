package productruntime

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/open-octo/octo-agent/internal/productclient"
	"github.com/open-octo/octo-agent/internal/productstate"
)

func TestCapabilityCachePartitionsCredentialsAndEnvironment(t *testing.T) {
	t.Setenv("OCTO_DATA_ROOT", t.TempDir())
	state, err := productstate.Open(productstate.Options{})
	if err != nil {
		t.Fatal(err)
	}
	if err = state.ApplyLogin(productstate.LoginOutcome{}); err != nil {
		t.Fatal(err)
	}
	tokens := signedIn("user-one")
	var hits atomic.Int32
	var fail atomic.Bool
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits.Add(1)
		if fail.Load() {
			w.WriteHeader(503)
			fmt.Fprint(w, `{"code":"SERVICE_UNAVAILABLE","message":"fixture offline"}`)
			return
		}
		json.NewEncoder(w).Encode(map[string]any{"data": map[string]any{"experts": []any{map[string]any{"id": r.Header.Get("Authorization"), "version": 1}}}})
	}))
	defer upstream.Close()
	rt := New(Deps{State: state, Platform: productclient.New(upstream.URL, productclient.ClientMeta{}, tokens)})
	call := func(path string) (int, map[string]any) {
		out := httptest.NewRecorder()
		rt.handleExperts(out, httptest.NewRequest("GET", path, nil))
		var body map[string]any
		_ = json.Unmarshal(out.Body.Bytes(), &body)
		return out.Code, body
	}
	code, first := call("/experts")
	if code != 200 || first["cached"] != false {
		t.Fatalf("first %d %v", code, first)
	}
	_, cached := call("/experts")
	if cached["cached"] != true || hits.Load() != 1 {
		t.Fatalf("cache miss %v %d", cached, hits.Load())
	}
	fail.Store(true)
	code, _ = call("/experts?refresh=1")
	if code == 200 {
		t.Fatal("failed refresh reported success")
	}
	_, retained := call("/experts")
	if retained["cached"] != true {
		t.Fatal("failed refresh discarded cache")
	}
	fail.Store(false)
	tokens.Set(productclient.Credentials{AccessToken: "user-two"})
	_, second := call("/experts")
	if second["cached"] != false || !strings.Contains(fmt.Sprint(second), "user-two") {
		t.Fatalf("cross-account cache %v", second)
	}
	rt.deps.Platform = productclient.New(upstream.URL+"/other-environment", productclient.ClientMeta{}, tokens)
	_, environment := call("/experts")
	if environment["cached"] != false {
		t.Fatal("cross-environment cache reused")
	}
}

func TestLoadPlatformSkillRequiresPinnedAssociationAndPropagatesFailure(t *testing.T) {
	t.Setenv("OCTO_DATA_ROOT", t.TempDir())
	state, err := productstate.Open(productstate.Options{})
	if err != nil {
		t.Fatal(err)
	}
	if err = state.ApplyLogin(productstate.LoginOutcome{}); err != nil {
		t.Fatal(err)
	}
	var fail atomic.Bool
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/client/experts/expert" || r.URL.Query().Get("version") != "3" {
			t.Errorf("wrong pinned request %s", r.URL)
		}
		if fail.Load() {
			w.WriteHeader(503)
			fmt.Fprint(w, `{"code":"SERVICE_UNAVAILABLE","message":"fixture offline"}`)
			return
		}
		fmt.Fprint(w, `{"data":{"id":"expert","version":3,"skills":[{"id":"skill","name":"Review","version":2,"content":"PINNED_INSTRUCTION"}]}}`)
	}))
	defer upstream.Close()
	rt := New(Deps{State: state, Platform: productclient.New(upstream.URL, productclient.ClientMeta{}, signedIn("test"))})
	result, err := rt.LoadPlatformSkill(context.Background(), "expert", 3, "skill")
	if err != nil || !strings.Contains(result, "PINNED_INSTRUCTION") {
		t.Fatalf("result %q err %v", result, err)
	}
	if _, err = rt.LoadPlatformSkill(context.Background(), "expert", 3, "unbound"); err == nil {
		t.Fatal("unbound skill accepted")
	}
	fail.Store(true)
	rt.capabilities.mu.Lock()
	rt.capabilities.entries = map[string]capabilitySnapshot{}
	rt.capabilities.mu.Unlock()
	if _, err = rt.LoadPlatformSkill(context.Background(), "expert", 3, "skill"); err == nil {
		t.Fatal("failed loader returned success")
	}
}

func TestPlatformSkillDetailSingleFlightBoundedCacheAndExpiry(t *testing.T) {
	t.Setenv("OCTO_DATA_ROOT", t.TempDir())
	state, err := productstate.Open(productstate.Options{})
	if err != nil {
		t.Fatal(err)
	}
	if err = state.ApplyLogin(productstate.LoginOutcome{}); err != nil {
		t.Fatal(err)
	}
	var hits atomic.Int32
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits.Add(1)
		time.Sleep(30 * time.Millisecond)
		fmt.Fprint(w, `{"data":{"id":"expert","version":3,"skills":[{"id":"skill","name":"Review","version":2,"content":"PINNED"}]}}`)
	}))
	defer upstream.Close()
	tokens := signedIn("account-one")
	rt := New(Deps{State: state, Platform: productclient.New(upstream.URL, productclient.ClientMeta{}, tokens)})
	start := time.Now()
	var wg sync.WaitGroup
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if _, err := rt.LoadPlatformSkill(context.Background(), "expert", 3, "skill"); err != nil {
				t.Error(err)
			}
		}()
	}
	wg.Wait()
	cold := time.Since(start)
	if hits.Load() != 1 {
		t.Fatalf("concurrent detail reads = %d", hits.Load())
	}
	start = time.Now()
	for i := 0; i < 10; i++ {
		if _, err := rt.LoadPlatformSkill(context.Background(), "expert", 3, "skill"); err != nil {
			t.Fatal(err)
		}
	}
	t.Logf("fixture cold concurrent load=%s, ten cached loads=%s, upstream requests=%d", cold, time.Since(start), hits.Load())
	if hits.Load() != 1 {
		t.Fatal("cached skill issued network request")
	}
	rt.capabilities.mu.Lock()
	for k, v := range rt.capabilities.entries {
		v.updated = time.Now().Add(-2 * time.Minute)
		rt.capabilities.entries[k] = v
	}
	rt.capabilities.mu.Unlock()
	if _, err := rt.LoadPlatformSkill(context.Background(), "expert", 3, "skill"); err != nil {
		t.Fatal(err)
	}
	if hits.Load() != 2 {
		t.Fatal("expired skill did not refresh")
	}
	tokens.Set(productclient.Credentials{AccessToken: "account-two"})
	if _, err := rt.LoadPlatformSkill(context.Background(), "expert", 3, "skill"); err != nil {
		t.Fatal(err)
	}
	if hits.Load() != 3 {
		t.Fatal("detail crossed account boundary")
	}
	rt.capabilities.mu.Lock()
	for i := 0; i < 200; i++ {
		rt.capabilities.store(fmt.Sprint(i), capabilitySnapshot{updated: time.Now()})
	}
	size := len(rt.capabilities.entries)
	rt.capabilities.mu.Unlock()
	if size > 128 {
		t.Fatalf("unbounded cache %d", size)
	}
}

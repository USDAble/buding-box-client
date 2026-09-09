package server

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"

	"github.com/open-octo/octo-agent/internal/datapath"
)

// The P13 dictionary-management endpoints. All three are product-gated; the
// file data/sensitive-words.txt is the single source of truth, so these tests
// assert both the HTTP contract and that the file reflects the write.

func dictPath(t *testing.T) string {
	t.Helper()
	p, err := datapath.Join("sensitive-words.txt")
	if err != nil {
		t.Fatal(err)
	}
	return p
}

func dictDo(t *testing.T, srv *Server, method, path, body, token string) *httptest.ResponseRecorder {
	t.Helper()
	var req *http.Request
	if body == "" {
		req = httptest.NewRequest(method, path, nil)
	} else {
		req = httptest.NewRequest(method, path, strings.NewReader(body))
	}
	req = withWindowToken(req, token)
	w := httptest.NewRecorder()
	serveLoopback(srv.mux, w, req)
	return w
}

func TestSensitiveDictGetEmpty(t *testing.T) {
	productTestEnv(t)
	srv := mustServer(t, Config{Addr: "127.0.0.1:0", Tools: false, WindowToken: "tok"})
	login(t, srv)

	w := dictDo(t, srv, http.MethodGet, "/api/product/sensitive/dict", "", "tok")
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200 (body %s)", w.Code, w.Body.String())
	}
	var resp struct {
		Builtin []string `json:"builtin"`
		User    []string `json:"user"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatal(err)
	}
	if len(resp.Builtin) == 0 || len(resp.User) != 0 {
		t.Fatalf("builtin=%d user=%d, want non-empty builtin / empty user", len(resp.Builtin), len(resp.User))
	}
}

func TestSensitiveDictPutWritesFileAndRereads(t *testing.T) {
	productTestEnv(t)
	srv := mustServer(t, Config{Addr: "127.0.0.1:0", Tools: false, WindowToken: "tok"})
	login(t, srv)

	w := dictDo(t, srv, http.MethodPut, "/api/product/sensitive/dict", `{"user":["测试词","另一个"]}`, "tok")
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200 (body %s)", w.Code, w.Body.String())
	}

	// The file (the single source of truth) now holds the two words.
	data, err := os.ReadFile(dictPath(t))
	if err != nil {
		t.Fatalf("read dict file: %v", err)
	}
	if got := string(data); !strings.Contains(got, "测试词\n") || !strings.Contains(got, "另一个\n") {
		t.Fatalf("dict file = %q, want both words", got)
	}

	// A fresh GET reflects the hand-write (file is the truth, not a cache).
	w = dictDo(t, srv, http.MethodGet, "/api/product/sensitive/dict", "", "tok")
	var resp struct {
		User []string `json:"user"`
	}
	_ = json.Unmarshal(w.Body.Bytes(), &resp)
	if len(resp.User) != 2 {
		t.Fatalf("user after put = %q, want 2 entries", resp.User)
	}
}

func TestSensitiveDictPutRejectsInvalidWordWithoutWriting(t *testing.T) {
	productTestEnv(t)
	srv := mustServer(t, Config{Addr: "127.0.0.1:0", Tools: false, WindowToken: "tok"})
	login(t, srv)
	os.WriteFile(dictPath(t), []byte("已有词\n"), 0o600)

	w := dictDo(t, srv, http.MethodPut, "/api/product/sensitive/dict", `{"user":["已有词","!!!"]}`, "tok")
	if w.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400 (body %s)", w.Code, w.Body.String())
	}
	var body map[string]string
	_ = json.Unmarshal(w.Body.Bytes(), &body)
	if body["code"] != "invalid_word" || body["word"] != "!!!" {
		t.Fatalf("body = %v, want invalid_word/!!!", body)
	}
	// Refusal must not have touched the file.
	data, _ := os.ReadFile(dictPath(t))
	if string(data) != "已有词\n" {
		t.Fatalf("file changed after refusal: %q", data)
	}
}

func TestSensitiveDictPutPreservesHeaderComment(t *testing.T) {
	productTestEnv(t)
	srv := mustServer(t, Config{Addr: "127.0.0.1:0", Tools: false, WindowToken: "tok"})
	login(t, srv)
	os.WriteFile(dictPath(t), []byte("# 我的说明\n旧词\n"), 0o600)

	w := dictDo(t, srv, http.MethodPut, "/api/product/sensitive/dict", `{"user":["新词"]}`, "tok")
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200 (body %s)", w.Code, w.Body.String())
	}
	data, _ := os.ReadFile(dictPath(t))
	if !strings.Contains(string(data), "# 我的说明\n") {
		t.Fatalf("header comment not preserved: %q", data)
	}
}

func TestSensitiveDictImportMergeNotOverwrite(t *testing.T) {
	productTestEnv(t)
	srv := mustServer(t, Config{Addr: "127.0.0.1:0", Tools: false, WindowToken: "tok"})
	login(t, srv)
	os.WriteFile(dictPath(t), []byte("原有词\n"), 0o600)

	// dryRun preview: 新增 1 (香蕉), 跳过 2 (原有词 duplicate + 纯符号).
	w := dictDo(t, srv, http.MethodPost, "/api/product/sensitive/dict/import", `{"words":["原有词","香蕉","!!!"],"dryRun":true}`, "tok")
	if w.Code != http.StatusOK {
		t.Fatalf("dryRun status = %d, want 200 (body %s)", w.Code, w.Body.String())
	}
	var preview struct {
		Added   int `json:"added"`
		Skipped int `json:"skipped"`
	}
	_ = json.Unmarshal(w.Body.Bytes(), &preview)
	if preview.Added != 1 || preview.Skipped != 2 {
		t.Fatalf("preview = %+v, want added=1 skipped=2", preview)
	}
	// dryRun must not have written.
	data, _ := os.ReadFile(dictPath(t))
	if string(data) != "原有词\n" {
		t.Fatalf("file changed on dryRun: %q", data)
	}

	// Real import merges (keeps 原有词, adds 香蕉).
	w = dictDo(t, srv, http.MethodPost, "/api/product/sensitive/dict/import", `{"words":["原有词","香蕉","!!!"],"dryRun":false}`, "tok")
	if w.Code != http.StatusOK {
		t.Fatalf("import status = %d, want 200 (body %s)", w.Code, w.Body.String())
	}
	data, _ = os.ReadFile(dictPath(t))
	if !strings.Contains(string(data), "原有词\n") || !strings.Contains(string(data), "香蕉\n") {
		t.Fatalf("file after import = %q, want merged", data)
	}
}

func TestSensitiveDictImportSkipsBuiltinDuplicate(t *testing.T) {
	productTestEnv(t)
	srv := mustServer(t, Config{Addr: "127.0.0.1:0", Tools: false, WindowToken: "tok"})
	login(t, srv)

	// 发票 is a built-in word; importing it (even with whitespace) skips it.
	w := dictDo(t, srv, http.MethodPost, "/api/product/sensitive/dict/import", `{"words":["发 票"],"dryRun":true}`, "tok")
	var preview struct {
		Added   int `json:"added"`
		Skipped int `json:"skipped"`
	}
	_ = json.Unmarshal(w.Body.Bytes(), &preview)
	if preview.Added != 0 || preview.Skipped != 1 {
		t.Fatalf("preview = %+v, want added=0 skipped=1", preview)
	}
}

func TestSensitiveDictGated(t *testing.T) {
	productTestEnv(t)
	srv := mustServer(t, Config{Addr: "127.0.0.1:0", Tools: false, WindowToken: "tok"})

	w := dictDo(t, srv, http.MethodGet, "/api/product/sensitive/dict", "", "tok")
	if w.Code != http.StatusForbidden {
		t.Fatalf("status = %d, want 403 for a not-logged-in window", w.Code)
	}
}

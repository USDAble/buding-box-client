// OCTO-FORK: L-D5 control-plane dictionary download — see dev-docs-usdable/需求/20260911/需求基线.md D5.
package productclient

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestSensitiveDictionaryFetchesTheKnownVersion(t *testing.T) {
	var gotPath, gotBearer string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.RequestURI()
		gotBearer = r.Header.Get("Authorization")
		_ = json.NewEncoder(w).Encode(map[string]any{"data": map[string]any{
			"dictionary": map[string]any{
				"version": "43", "mode": "delta", "baseVersion": "42",
				"add": []string{"new-word"}, "remove": []string{"old-word"},
				"sha256": "digest", "keyId": "dictionary-key", "audience": "product",
				"issuedAt": "2026-09-10T08:00:00Z", "expiresAt": "2026-10-10T08:00:00Z",
			},
			"dictionarySignature": map[string]any{"keyId": "dictionary-key", "sig": "signature"},
		}})
	}))
	defer srv.Close()

	creds := &CredentialHolder{}
	creds.Set(Credentials{AccessToken: "access"})
	client := New(srv.URL, ClientMeta{}, creds)
	got, err := client.SensitiveDictionary(context.Background(), "42")
	if err != nil {
		t.Fatalf("SensitiveDictionary: %v", err)
	}
	if gotPath != "/dictionaries/sensitive?version=42" {
		t.Errorf("path = %q", gotPath)
	}
	if gotBearer != "Bearer access" {
		t.Errorf("Authorization = %q", gotBearer)
	}
	var dictionary SensitiveDictionary
	if err := json.Unmarshal(got.Dictionary, &dictionary); err != nil {
		t.Fatalf("decode dictionary: %v", err)
	}
	if dictionary.Version != "43" || dictionary.BaseVersion != "42" || len(dictionary.Add) != 1 || got.Unchanged {
		t.Errorf("decoded data = %#v", got)
	}
	if got.DictionarySignature.KeyID != "dictionary-key" || got.DictionarySignature.Sig != "signature" {
		t.Errorf("decoded signature = %#v", got.DictionarySignature)
	}
}

func TestSensitiveDictionaryFolds304IntoUnchanged(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusNotModified)
	}))
	defer srv.Close()

	creds := &CredentialHolder{}
	creds.Set(Credentials{AccessToken: "access"})
	got, err := New(srv.URL, ClientMeta{}, creds).SensitiveDictionary(context.Background(), "43")
	if err != nil {
		t.Fatalf("SensitiveDictionary: %v", err)
	}
	if !got.Unchanged {
		t.Fatalf("Unchanged = false, want true: %#v", got)
	}
}

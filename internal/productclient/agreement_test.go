package productclient

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestAgreementPublicPublication(t *testing.T) {
	version := "v1"
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/v1/client/agreements/privacy" || r.Method != "GET" {
			t.Errorf("unexpected request: %s %s", r.Method, r.URL.Path)
		}
		if r.Header.Get("Authorization") != "" {
			t.Error("public agreement leaked credentials")
		}
		fmt.Fprintf(w, `{"code":"OK","data":{"kind":"privacy","version":%q,"title":"Privacy","content":"line1\n<script>plain text</script>"}}`, version)
	}))
	defer ts.Close()
	holder := &CredentialHolder{}
	holder.Set(Credentials{AccessToken: "secret"})
	c := New(ts.URL+"/api/v1", ClientMeta{}, holder)
	for _, v := range []string{"v1", "v2"} {
		version = v
		a, err := c.Agreement(context.Background(), "privacy")
		if err != nil {
			t.Fatal(err)
		}
		if a.Version != v || a.Content != "line1\n<script>plain text</script>" {
			t.Fatalf("wrong publication: %+v", a)
		}
	}
}

func TestAgreementRejectsFailures(t *testing.T) {
	for _, tc := range []struct {
		status int
		body   string
	}{
		{404, `{"code":"not_found"}`},
		{500, `upstream failure`},
		{200, `not json`},
		{200, `{"code":"FAILED","data":{"kind":"box","version":"v1","title":"Terms","content":"text"}}`},
		{200, `{"code":200,"data":{"kind":"box","version":"v1","title":"Terms","content":"text"}}`},
		{200, `{"code":500,"data":{"kind":"box","version":"v1","title":"Terms","content":"text"}}`},
		{200, `{"code":200,"data":{"kind":"privacy","version":"v1","title":"Terms","content":"text"}}`},
		{200, `{"code":200,"data":{"kind":"box","version":"","title":"Terms","content":"text"}}`},
	} {
		ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(tc.status); fmt.Fprint(w, tc.body) }))
		c := New(ts.URL, ClientMeta{}, &CredentialHolder{})
		_, err := c.Agreement(context.Background(), "box")
		ts.Close()
		if err == nil {
			t.Fatalf("accepted invalid publication: %s", tc.body)
		}
	}
	if _, err := New("", ClientMeta{}, nil).Agreement(context.Background(), "../secret"); err == nil {
		t.Fatal("invalid kind accepted")
	}
}

func TestAgreementDoesNotCallLegacyRoute(t *testing.T) {
	requests := 0
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests++
		if r.URL.Path != "/api/v1/client/agreements/box" {
			t.Errorf("unexpected path: %s", r.URL.Path)
		}
		http.NotFound(w, r)
	}))
	defer ts.Close()
	_, err := New(ts.URL+"/api/v1", ClientMeta{}, &CredentialHolder{}).Agreement(context.Background(), "box")
	if err == nil || requests != 1 {
		t.Fatalf("new route failure must not call legacy route: requests=%d, err=%v", requests, err)
	}
}

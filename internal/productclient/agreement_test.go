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
		if r.URL.Path != "/api/v1/agreements/privacy" || r.Method != "GET" {
			t.Errorf("unexpected request: %s %s", r.Method, r.URL.Path)
		}
		if r.Header.Get("Authorization") != "" {
			t.Error("public agreement leaked credentials")
		}
		fmt.Fprintf(w, `{"code":200,"data":{"kind":"privacy","version":%q,"title":"Privacy","content":"line1\n<script>plain text</script>"}}`, version)
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

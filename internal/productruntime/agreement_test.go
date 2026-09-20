package productruntime_test

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/open-octo/octo-agent/internal/productclient"
	"github.com/open-octo/octo-agent/internal/productruntime"
)

func TestAgreementAvailableBeforeLogin(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, `{"code":200,"data":{"kind":"box","version":"v1","title":"Terms","content":"text"}}`)
	}))
	defer ts.Close()
	rt := productruntime.New(productruntime.Deps{Platform: productclient.New(ts.URL, productclient.ClientMeta{}, nil)})
	for _, tc := range []struct {
		kind   string
		status int
	}{{"box", 200}, {"other", 400}} {
		rec := httptest.NewRecorder()
		rt.Handler().ServeHTTP(rec, httptest.NewRequest("GET", "/api/product/agreements/"+tc.kind, nil))
		if rec.Code != tc.status || rec.Header().Get("Cache-Control") != "no-store" {
			t.Fatalf("%s: %d %s", tc.kind, rec.Code, rec.Body.String())
		}
	}
}

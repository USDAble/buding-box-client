package productclient_test

import (
	"context"
	"encoding/json"
	"github.com/open-octo/octo-agent/internal/productclient"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestUpdateNicknameUsesAuthenticatedAccountAndRejectsIncompleteReply(t *testing.T) {
	for _, test := range []struct {
		name, reply string
		fail        bool
	}{{"accepted", `{"data":{"nickname":"ServerName"}}`, false}, {"missing-name", `{"data":{}}`, true}} {
		t.Run(test.name, func(t *testing.T) {
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.URL.Path != "/v1/client/account/nickname" || r.Method != "PUT" || r.Header.Get("Authorization") != "Bearer token" {
					t.Errorf("incorrect account request")
				}
				var payload map[string]any
				json.NewDecoder(r.Body).Decode(&payload)
				if len(payload) != 1 || payload["nickname"] != "NewName" {
					t.Errorf("unexpected payload: %v", payload)
				}
				w.Header().Set("Content-Type", "application/json")
				w.Write([]byte(test.reply))
			}))
			defer srv.Close()
			holder := &productclient.CredentialHolder{}
			holder.Set(productclient.Credentials{AccessToken: "token"})
			client := productclient.New(srv.URL+"/v1", productclient.ClientMeta{}, holder)
			out, err := client.UpdateNickname(context.Background(), "NewName")
			if (err != nil) != test.fail {
				t.Fatalf("error=%v", err)
			}
			if !test.fail && out.Nickname != "ServerName" {
				t.Fatal("server result not returned")
			}
		})
	}
}

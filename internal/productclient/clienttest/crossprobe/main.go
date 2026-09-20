// Command crossprobe drives an isolated platform HTTP test with real client code.
// Its temporary data root contains test credentials only, never user data.
package main

import (
	"context"
	"fmt"

	"github.com/open-octo/octo-agent/internal/credentialstore"
	"github.com/open-octo/octo-agent/internal/productclient"
	"os"
	"time"
)

func must(err error) {
	if err != nil {
		panic(err)
	}
}
func check(ok bool, message string) {
	if !ok {
		panic(message)
	}
}
func main() {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	root, err := os.MkdirTemp("", "buding-auth-cross-")
	must(err)
	defer os.RemoveAll(root)
	must(os.Setenv("OCTO_DATA_ROOT", root))
	meta := productclient.ClientMeta{Version: "cross-repo-test", InstallID: "9329da7b-ef0b-4aaa-8a12-0938c57c1418", Platform: "windows", Arch: "amd64"}
	holder := &productclient.CredentialHolder{}
	client := productclient.New(os.Args[1], meta, holder)
	sms, err := client.SendSMS(ctx, productclient.SendSMSRequest{Phone: "+8613800138000"})
	must(err)
	check(sms.CooldownSec > 0, "SMS envelope decoding failed")
	req := productclient.LoginRequest{Phone: "+8613800138000", Code: "482915", Nickname: "Cross repo", ActivationCode: "2222-AAAA-3333-BBBB", BoxCode: os.Args[2], InstallID: meta.InstallID, ClientRequestID: "9329da7b-ef0b-4aaa-8a12-0938c57c1418"}
	login, err := client.Login(ctx, req)
	must(err)
	check(login.Account.ID != "" && holder.AccessToken() != "", "login did not populate credentials")
	original := holder.Get().RefreshToken
	store, err := credentialstore.Open(credentialstore.Options{})
	must(err)
	must(store.Save(credentialstore.Credential{RefreshToken: original, InstallID: meta.InstallID}))
	holder.Clear()
	reopened, err := credentialstore.Open(credentialstore.Options{})
	must(err)
	saved, found, err := reopened.Load()
	must(err)
	check(found && saved.RefreshToken == original, "portable credentials not restored")
	restoredHolder := &productclient.CredentialHolder{}
	restored := productclient.New(os.Args[1], meta, restoredHolder)
	rotated, err := restored.Refresh(ctx, saved.RefreshToken)
	must(err)
	check(rotated.RefreshToken != original, "refresh did not rotate")
	restoredHolder.Set(productclient.Credentials{AccessToken: rotated.AccessToken, RefreshToken: rotated.RefreshToken, ExpiresAt: time.Now().Add(time.Duration(rotated.AccessTokenExpiresInSec) * time.Second)})
	must(restored.Logout(ctx))
	check(restoredHolder.AccessToken() == "", "logout did not clear holder")
	_, err = restored.Refresh(ctx, rotated.RefreshToken)
	check(err != nil, "revoked session still refreshes")
	fmt.Println("PASS real client -> middle HTTP: SMS, activation/login, portable credential save/reopen, refresh rotation, logout, revoked refresh rejection")
}

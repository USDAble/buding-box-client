// phase2probe calls the middle-tier's isolated test HTTP server with real client code.
package main

import (
	"context"
	"encoding/json"
	"fmt"
	"github.com/open-octo/octo-agent/internal/productclient"
	"os"
	"strings"
	"testing"
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
func runProbe() {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	const install = "9329da7b-ef0b-4aaa-8a12-0938c57c1418"
	holder := &productclient.CredentialHolder{}
	client := productclient.New(os.Args[1], productclient.ClientMeta{Version: "phase2-test", InstallID: install, Platform: "windows", Arch: "amd64"}, holder)
	_, err := client.SendSMS(ctx, productclient.SendSMSRequest{Phone: "+8613800138000"})
	must(err)
	req := productclient.LoginRequest{Phone: "+8613800138000", Code: "482915", Nickname: "Original", ActivationCode: "2222-AAAA-3333-BBBB", BoxCode: os.Args[2], InstallID: install, ClientRequestID: "bb87f8b4-c193-46cb-b25a-4067fd4099b1"}
	login, err := client.Login(ctx, req)
	must(err)
	check(login.Account.ID != "", "missing account")
	ledger, err := client.CreditsLedger(ctx)
	must(err)
	check(ledger.BalanceMicroCredits != nil && *ledger.BalanceMicroCredits == 12345600, "wallet precision/envelope mismatch")
	walletRaw, err := client.FinanceRead(ctx, "wallet", nil)
	must(err)
	var wallet struct {
		Available string `json:"available_points"`
		Reserved  string `json:"reserved_points"`
	}
	must(json.Unmarshal(walletRaw, &wallet))
	check(wallet.Available == "12.3456" && wallet.Reserved == "1.0000", "finance direct DTO does not match real wallet")
	box, err := client.Box(ctx)
	must(err)
	check(box.ID == os.Args[6], "wrong box identity")
	check(box.State == "unknown", "fabricated online state")
	check(box.Capabilities.PrivateModels.State == "unavailable", "fabricated private capability")
	catalog, err := client.CatalogModels(ctx, "")
	must(err)
	policy, err := catalog.PolicyEnvelope.Verify(productclient.VerifyOptions{TrustedKeys: map[string]string{"cross-test": os.Args[5]}, Audience: "puddingbox", Now: time.Now()})
	must(err)
	check(len(policy.Catalog.Models) == 1 && policy.Catalog.Models[0].ID == os.Args[7], "catalog model mapping mismatch")
	check(!policy.Catalog.Models[0].Confidential, "box execution is not confidentiality")
	same, err := client.CatalogModels(ctx, policy.Catalog.Version)
	must(err)
	renewedPolicy, err := same.PolicyEnvelope.Verify(productclient.VerifyOptions{TrustedKeys: map[string]string{"cross-test": os.Args[5]}, Audience: "puddingbox", Now: time.Now()})
	must(err)
	check(renewedPolicy.Catalog.Version > policy.Catalog.Version && len(renewedPolicy.Catalog.Models) == 1, "conditional signed refresh not monotonic")
	_, err = catalog.PolicyEnvelope.Verify(productclient.VerifyOptions{TrustedKeys: map[string]string{"cross-test": os.Args[5]}, Audience: "wrong-audience", Now: time.Now()})
	check(err != nil, "audience mismatch accepted")
	account, err := client.UpdateNickname(ctx, "Updated_cross")
	must(err)
	check(account.Nickname == "Updated_cross", "nickname response mismatch")
	dictionary, err := client.SensitiveDictionary(ctx, "")
	must(err)
	snapshot, err := dictionary.SensitiveDictionaryEnvelope.Verify(productclient.VerifyOptions{TrustedKeys: map[string]string{"cross-test": os.Args[5]}, Audience: "puddingbox", Now: time.Now()})
	must(err)
	check(snapshot.Mode == "full" && len(snapshot.Words) == 1 && snapshot.Words[0] == "测试补充词", "dictionary payload mismatch")
	check(snapshot.SHA256 == productclient.SensitiveDictionarySHA256(snapshot.Words), "dictionary checksum mismatch")
	feedback := productclient.FeedbackRequest{Category: "bug", Title: "Cross test", Content: "Explicit user form", Impact: "normal"}
	const feedbackKey = "edc72c49-eaca-4975-a4d3-4c696a6295b3"
	receipt, err := client.Feedback(ctx, feedback, feedbackKey)
	must(err)
	replay, err := client.Feedback(ctx, feedback, feedbackKey)
	must(err)
	check(receipt.FeedbackID != "" && *receipt == *replay, "feedback receipt not idempotent")
	feedback.Content = "Changed payload"
	_, err = client.Feedback(ctx, feedback, feedbackKey)
	check(err != nil, "feedback idempotency conflict accepted")
	must(client.Logout(ctx))
	_, err = client.SendSMS(ctx, productclient.SendSMSRequest{Phone: req.Phone})
	must(err)
	req.ActivationCode, req.BoxCode, req.Nickname = "", "", ""
	req.ClientRequestID = "954c9e5c-a62d-45e0-9b31-a2a9c4f6a2d2"
	relogin, err := client.Login(ctx, req)
	must(err)
	check(relogin.Account.Nickname == "Updated_cross", "nickname not durable across SMS login")
	fmt.Println("PASS real client: signed catalog/dictionary, exact wallet, owned box, nickname persistence, feedback receipt/idempotency")
}

func TestMain(m *testing.M) {
	if len(os.Args) > 1 && strings.HasPrefix(os.Args[1], "http://") {
		runProbe()
		return
	}
	os.Exit(m.Run())
}

package productclient_test

import (
	"context"
	"errors"
	"net/http/httptest"
	"testing"

	"github.com/open-octo/octo-agent/internal/productclient"
	"github.com/open-octo/octo-agent/internal/productclient/clienttest"
)

const (
	installID    = "6f1c0f6e-3f2a-4a4c-9f3f-2b6b1c7a9e01"
	smsCode      = clienttest.FixtureSMSCode
	anyPhone     = "13900001111"
	otherPhone   = "13700002222"
	requestIDOne = "0b6f0f1e-0000-4000-8000-000000000001"
)

type harness struct {
	standin *clienttest.Server
	creds   *productclient.CredentialHolder
	client  *productclient.Client
}

func newHarness(t *testing.T) *harness {
	t.Helper()
	standin := clienttest.New()
	server := httptest.NewServer(standin.Handler())
	t.Cleanup(server.Close)

	creds := &productclient.CredentialHolder{}
	// The base URL is versioned, because that is the only shape a control-plane
	// host ever has: production.json ships `https://api.invalid/v1` and a profile
	// test asserts the /v1 suffix. Passing the bare origin here (as this harness
	// used to) hides any disagreement about who owns the version segment.
	client := productclient.New(server.URL+"/v1", productclient.ClientMeta{
		Version:   "test",
		Platform:  "windows",
		Arch:      "amd64",
		InstallID: installID,
	}, creds)

	return &harness{standin: standin, creds: creds, client: client}
}

// requestCode mirrors the UI: the code is requested before any login attempt.
func (h *harness) requestCode(t *testing.T, phone string) {
	t.Helper()
	data, err := h.client.SendSMS(context.Background(), productclient.SendSMSRequest{
		Phone:           phone,
		ClientRequestID: requestIDOne,
	})
	if err != nil {
		t.Fatalf("SendSMS: %v", err)
	}
	if data.CooldownSec <= 0 {
		t.Fatalf("CooldownSec = %d, want > 0", data.CooldownSec)
	}
}

// login performs the five-field first activation (需求基线 E1).
func (h *harness) activate(t *testing.T, phone, activationCode, boxCode string) (*productclient.LoginData, error) {
	t.Helper()
	h.requestCode(t, phone)
	return h.client.Login(context.Background(), productclient.LoginRequest{
		Phone:           phone,
		Code:            smsCode,
		Nickname:        "tester",
		ActivationCode:  activationCode,
		BoxCode:         boxCode,
		InstallID:       installID,
		ClientRequestID: requestIDOne,
	})
}

func code(err error) string {
	var apiErr *productclient.Error
	if errors.As(err, &apiErr) {
		return apiErr.Code
	}
	return ""
}

// L-A1: first activation succeeds, stores credentials, and the box code comes
// back from the server rather than from the form.
func TestFirstActivationStoresCredentialsAndReturnsBoxCode(t *testing.T) {
	h := newHarness(t)

	data, err := h.activate(t, anyPhone, clienttest.FixtureActivationCode, clienttest.FixtureBoxCode)
	if err != nil {
		t.Fatalf("Login: %v", err)
	}
	if data.Activation == nil {
		t.Fatal("Activation is nil after first activation")
	}
	if data.Activation.BoxCode != clienttest.FixtureBoxCode {
		t.Errorf("Activation.BoxCode = %q, want %q", data.Activation.BoxCode, clienttest.FixtureBoxCode)
	}
	if got := h.creds.Get(); got.AccessToken == "" || got.RefreshToken == "" {
		t.Errorf("credentials not stored: %+v", got)
	}
}

// L-A1 one-to-many: one box code serves more than one activation code.
func TestOneBoxCodeAcceptsTwoActivationCodes(t *testing.T) {
	h := newHarness(t)
	ctx := context.Background()

	if _, err := h.activate(t, anyPhone, clienttest.FixtureActivationCode, clienttest.FixtureBoxCode); err != nil {
		t.Fatalf("first activation: %v", err)
	}

	// A different person, a different code, the same box.
	h.requestCode(t, otherPhone)
	second, err := h.client.Login(ctx, productclient.LoginRequest{
		Phone:           otherPhone,
		Code:            smsCode,
		Nickname:        "second",
		ActivationCode:  clienttest.FixtureSecondActivationCode,
		BoxCode:         clienttest.FixtureBoxCode,
		InstallID:       installID,
		ClientRequestID: requestIDOne,
	})
	if err != nil {
		t.Fatalf("second activation on the same box code failed: %v", err)
	}
	if second.Activation == nil || second.Activation.BoxCode != clienttest.FixtureBoxCode {
		t.Errorf("second activation box code = %+v, want %q", second.Activation, clienttest.FixtureBoxCode)
	}
}

// L-A2: a later login carries neither the activation code nor the box code, and
// still reports the activation the account already holds.
func TestLaterLoginOmitsActivationAndStillReportsIt(t *testing.T) {
	h := newHarness(t)
	ctx := context.Background()

	if _, err := h.activate(t, anyPhone, clienttest.FixtureActivationCode, clienttest.FixtureBoxCode); err != nil {
		t.Fatalf("first activation: %v", err)
	}
	h.creds.Clear()

	h.requestCode(t, anyPhone)
	data, err := h.client.Login(ctx, productclient.LoginRequest{
		Phone:           anyPhone,
		Code:            smsCode,
		InstallID:       installID,
		ClientRequestID: requestIDOne,
	})
	if err != nil {
		t.Fatalf("later login: %v", err)
	}
	if data.Activation == nil || data.Activation.BoxCode != clienttest.FixtureBoxCode {
		t.Errorf("later login lost the activation record: %+v", data.Activation)
	}
}

// L-A4: the four activation failures must stay distinguishable. Collapsing them
// into one message is what leaves a user stuck after a single mistyped digit.
func TestActivationFailuresStayDistinguishable(t *testing.T) {
	cases := []struct {
		name           string
		phone          string
		activationCode string
		boxCode        string
		want           string
	}{
		{
			name:           "code the platform never issued",
			phone:          anyPhone,
			activationCode: "BUDING-DEMO-9999",
			boxCode:        clienttest.FixtureBoxCode,
			want:           productclient.CodeActivationInvalid,
		},
		{
			name:           "box code nobody was issued with",
			phone:          anyPhone,
			activationCode: clienttest.FixtureActivationCode,
			boxCode:        clienttest.FixtureUnknownBoxCode,
			want:           productclient.CodeBoxCodeUnknown,
		},
		{
			name:           "real box code, wrong pairing",
			phone:          anyPhone,
			activationCode: clienttest.FixtureActivationCode,
			boxCode:        clienttest.FixtureOtherBoxCode,
			want:           productclient.CodeBoxCodeMismatch,
		},
		{
			name:           "activation code issued to someone else",
			phone:          anyPhone,
			activationCode: clienttest.FixtureBoundPhoneActivationCode,
			boxCode:        clienttest.FixtureOtherBoxCode,
			want:           productclient.CodePhoneMismatch,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			h := newHarness(t)
			_, err := h.activate(t, tc.phone, tc.activationCode, tc.boxCode)
			if got := code(err); got != tc.want {
				t.Fatalf("code = %q, want %q (err: %v)", got, tc.want, err)
			}
		})
	}
}

// L-A4: a used code reports "already used", not "invalid".
func TestUsedActivationCodeIsReportedAsUsed(t *testing.T) {
	h := newHarness(t)

	if _, err := h.activate(t, anyPhone, clienttest.FixtureActivationCode, clienttest.FixtureBoxCode); err != nil {
		t.Fatalf("first activation: %v", err)
	}

	h.requestCode(t, otherPhone)
	_, err := h.client.Login(context.Background(), productclient.LoginRequest{
		Phone:           otherPhone,
		Code:            smsCode,
		ActivationCode:  clienttest.FixtureActivationCode,
		BoxCode:         clienttest.FixtureBoxCode,
		InstallID:       installID,
		ClientRequestID: requestIDOne,
	})
	if got := code(err); got != productclient.CodeActivationCodeUsed {
		t.Fatalf("code = %q, want %q", got, productclient.CodeActivationCodeUsed)
	}
}

// L-A5: a record written before the box code existed surfaces it as absent
// instead of failing - the UI renders a dash.
func TestActivationWithoutBoxCodeIsNotAnError(t *testing.T) {
	h := newHarness(t)

	if _, err := h.activate(t, anyPhone, clienttest.FixtureActivationCode, clienttest.FixtureBoxCode); err != nil {
		t.Fatalf("first activation: %v", err)
	}
	h.standin.ClearBoxCode(anyPhone)

	data, err := h.client.Bootstrap(context.Background())
	if err != nil {
		t.Fatalf("Bootstrap: %v", err)
	}
	if data.Activation == nil {
		t.Fatal("Activation is nil")
	}
	if data.Activation.BoxCode != "" {
		t.Errorf("Activation.BoxCode = %q, want empty", data.Activation.BoxCode)
	}
}

// L-A6: an expired access token is refreshed exactly once and the original call
// is replayed, with the refresh token rotated.
func TestExpiredAccessTokenRefreshesOnceAndReplays(t *testing.T) {
	h := newHarness(t)
	ctx := context.Background()

	if _, err := h.activate(t, anyPhone, clienttest.FixtureActivationCode, clienttest.FixtureBoxCode); err != nil {
		t.Fatalf("first activation: %v", err)
	}
	before := h.creds.Get()

	h.standin.ExpireAccessTokens()

	if _, err := h.client.Bootstrap(ctx); err != nil {
		t.Fatalf("Bootstrap after expiry: %v", err)
	}
	if got := h.standin.RefreshCount(); got != 1 {
		t.Errorf("refresh count = %d, want 1", got)
	}
	after := h.creds.Get()
	if after.AccessToken == before.AccessToken {
		t.Error("access token was not replaced")
	}
	if after.RefreshToken == before.RefreshToken {
		t.Error("refresh token was not rotated")
	}
}

// L-A6: when the refresh token itself is refused, the client clears its
// credentials and reports a session that cannot heal by retrying.
func TestRefusedRefreshClearsCredentials(t *testing.T) {
	h := newHarness(t)
	ctx := context.Background()

	if _, err := h.activate(t, anyPhone, clienttest.FixtureActivationCode, clienttest.FixtureBoxCode); err != nil {
		t.Fatalf("first activation: %v", err)
	}

	h.standin.ExpireAccessTokens()
	// Simulate a revoked session: the refresh token the client holds is gone.
	h.creds.Set(productclient.Credentials{AccessToken: "stale", RefreshToken: "revoked"})

	_, err := h.client.Bootstrap(ctx)
	if !errors.Is(err, productclient.ErrSessionExpired) {
		t.Fatalf("err = %v, want ErrSessionExpired", err)
	}
	if got := h.creds.Get(); got.AccessToken != "" || got.RefreshToken != "" {
		t.Errorf("credentials survived a refused refresh: %+v", got)
	}
	if got := h.standin.RefreshCount(); got != 1 {
		t.Errorf("refresh count = %d, want 1 - a refused refresh must not retry", got)
	}
}

// The code-not-sent and wrong-code paths are what the user hits most often, so
// they must not be reported as an activation failure.
func TestLoginCodeErrorsAreDistinct(t *testing.T) {
	h := newHarness(t)
	ctx := context.Background()

	_, err := h.client.Login(ctx, productclient.LoginRequest{
		Phone:           anyPhone,
		Code:            smsCode,
		InstallID:       installID,
		ClientRequestID: requestIDOne,
	})
	if got := code(err); got != productclient.CodeCodeNotSent {
		t.Errorf("before requesting a code: code = %q, want %q", got, productclient.CodeCodeNotSent)
	}

	h.requestCode(t, anyPhone)
	_, err = h.client.Login(ctx, productclient.LoginRequest{
		Phone:           anyPhone,
		Code:            "000000",
		InstallID:       installID,
		ClientRequestID: requestIDOne,
	})
	if got := code(err); got != productclient.CodeInvalidCode {
		t.Errorf("wrong code: code = %q, want %q", got, productclient.CodeInvalidCode)
	}
}

// A platform that is not listening at all is network_unavailable, which is a
// different fact from an upstream 5xx and is never a reason to fall back to a
// local model source.
func TestUnreachablePlatformIsNetworkUnavailable(t *testing.T) {
	server := httptest.NewServer(nil)
	url := server.URL
	server.Close() // nothing is listening now

	creds := &productclient.CredentialHolder{}
	client := productclient.New(url, productclient.ClientMeta{InstallID: installID}, creds)

	_, err := client.SendSMS(context.Background(), productclient.SendSMSRequest{
		Phone:           anyPhone,
		ClientRequestID: requestIDOne,
	})
	if got := code(err); got != productclient.CodeNetworkUnavailable {
		t.Fatalf("code = %q, want %q (err: %v)", got, productclient.CodeNetworkUnavailable, err)
	}
}

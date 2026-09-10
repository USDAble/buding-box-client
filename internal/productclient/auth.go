package productclient

import "context"

// authClient implements AuthClient over the shared Transport.
//
// It holds no token state on purpose. Which token to send is the Transport's
// business (it is the one that sets the Authorization header and coordinates the
// 401 refresh single-flight), and where a token is *stored* is
// internal/credentialstore's. An AuthClient that cached a token as well would be
// a second answer to "what is the current access token".
type authClient struct {
	t *Transport
}

// NewAuthClient returns the phone + SMS login surface over tr.
func NewAuthClient(tr *Transport) AuthClient { return &authClient{t: tr} }

// SendCode asks for an SMS code.
//
// Two things are filled in rather than asked of the caller:
//
//   - the correlation id, because the frozen signature has no parameter for it
//     and the platform expects one on every side-effecting call;
//   - `purpose`, which is `login` for every flow this client has. It stays a
//     field rather than being dropped from the DTO because the platform may
//     later distinguish flows, and guessing then would be worse than sending the
//     one value we have.
//
// The call is CallWrite, so it is never automatically retried: a retry here is a
// second SMS the user did not ask for (中台交付包 §3.3).
func (c *authClient) SendCode(ctx context.Context, phone string) (Cooldown, error) {
	req := SendCodeRequest{
		Phone:           phone,
		Purpose:         "login",
		ClientRequestID: NewClientRequestID(),
	}
	var out Cooldown
	err := c.t.Do(ctx, Request{
		Op:              "SendCode",
		Method:          "POST",
		Path:            "/auth/sms/send",
		Kind:            CallWrite,
		Body:            req,
		ClientRequestID: req.ClientRequestID,
		Out:             &out,
	})
	return out, err
}

// Login performs first activation (ActivationCode set) or a later login.
//
// InstallID is filled from the transport when the caller leaves it empty: the
// same identifier already travels as X-Install-Id on this very request, and the
// platform correlates the two. Letting a caller pass a different one would make
// the header and the body describe different installs, with no error to show for
// it.
func (c *authClient) Login(ctx context.Context, req LoginRequest) (LoginResult, error) {
	if req.InstallID == "" {
		req.InstallID = c.t.InstallID()
	}
	if req.ClientRequestID == "" {
		req.ClientRequestID = NewClientRequestID()
	}
	var out LoginResult
	err := c.t.Do(ctx, Request{
		Op:              "Login",
		Method:          "POST",
		Path:            "/auth/login",
		Kind:            CallWrite,
		Body:            req,
		ClientRequestID: req.ClientRequestID,
		Out:             &out,
	})
	return out, err
}

// Refresh exchanges a refresh token for a new pair. The platform rotates the
// refresh token on every use (中台交付包 §4.2.3), so this is CallWrite: an
// automatic retry would spend the rotation twice and the second attempt would
// present a token the platform has already revoked.
//
// The returned Session is the caller's to persist — the credential store owns
// that, not this package.
//
// SkipAuthRefresh is what keeps a revoked session from hanging the client: this
// call is the recovery path, so its own 401 must be the final answer. See the
// field's comment.
func (c *authClient) Refresh(ctx context.Context, refreshToken string) (Session, error) {
	body := struct {
		RefreshToken string `json:"refreshToken"`
	}{RefreshToken: refreshToken}

	var out Session
	err := c.t.Do(ctx, Request{
		Op:              "Refresh",
		Method:          "POST",
		Path:            "/auth/refresh",
		Kind:            CallWrite,
		Body:            body,
		Out:             &out,
		SkipAuthRefresh: true,
	})
	return out, err
}

// Logout revokes the current session. It takes no arguments because the frozen
// signature has none: the platform identifies the session from the access token
// the transport sends.
//
// Locally the caller must clear credentials even when this returns an error —
// a logout the platform refused still means the user asked to be signed out, and
// leaving a usable refresh token on disk is the worse outcome. That is the
// caller's job (P0-02 §当前仓库开发任务 3), not something this method can
// enforce, so it is stated here.
func (c *authClient) Logout(ctx context.Context) error {
	return c.t.Do(ctx, Request{
		Op:     "Logout",
		Method: "POST",
		Path:   "/auth/logout",
		Kind:   CallWrite,
	})
}

package productclient

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"sync"
	"time"
)

// maxBody caps how much of a response is read, so a hostile or broken endpoint
// cannot exhaust memory.
const maxBody = 1 << 20

// Credentials are the tokens one signed-in account holds.
type Credentials struct {
	AccessToken  string
	RefreshToken string
	ExpiresAt    time.Time
}

// CredentialHolder is the in-memory credential store.
//
// The client never reads or writes a file. Persisting the refresh token belongs
// to the credential store, which is a separate component (需求基线 E6); keeping
// the client file-free is what lets it run in tests, and it matters that the
// access token only ever lives here (§C2 规则 1: it is never written to disk).
type CredentialHolder struct {
	mu    sync.RWMutex
	creds Credentials
}

// Set replaces the held credentials.
func (h *CredentialHolder) Set(c Credentials) {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.creds = c
}

// Get returns a copy of the held credentials.
func (h *CredentialHolder) Get() Credentials {
	h.mu.RLock()
	defer h.mu.RUnlock()
	return h.creds
}

// AccessToken returns the current access token, or "" when signed out.
func (h *CredentialHolder) AccessToken() string {
	h.mu.RLock()
	defer h.mu.RUnlock()
	return h.creds.AccessToken
}

// Clear drops every credential. It is what a rejected refresh leads to.
func (h *CredentialHolder) Clear() {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.creds = Credentials{}
}

// ClientMeta is the per-install identity every request carries (§3.1). InstallID
// is a random UUID generated once per installation - never a hardware serial.
type ClientMeta struct {
	Version   string
	Platform  string
	Arch      string
	InstallID string
}

// Client speaks the account lifecycle to one platform host.
type Client struct {
	baseURL string
	http    *http.Client
	meta    ClientMeta
	creds   *CredentialHolder

	now func() time.Time

	// refreshMu makes concurrent refreshes collapse into one call instead of a
	// storm (§C2 规则 4).
	refreshMu sync.Mutex
}

// Option customises a Client.
type Option func(*Client)

// WithHTTPClient swaps the transport, for tests and for a caller that needs its
// own timeouts.
func WithHTTPClient(h *http.Client) Option {
	return func(c *Client) { c.http = h }
}

// WithClock swaps the clock used to stamp token expiry.
func WithClock(now func() time.Time) Option {
	return func(c *Client) { c.now = now }
}

// New builds a client for baseURL (for example the platform host plus /v1).
// creds is required: the client holds tokens for exactly one account.
func New(baseURL string, meta ClientMeta, creds *CredentialHolder, opts ...Option) *Client {
	c := &Client{
		baseURL: baseURL,
		http:    &http.Client{Timeout: 30 * time.Second},
		meta:    meta,
		creds:   creds,
		now:     time.Now,
	}
	for _, opt := range opts {
		opt(c)
	}
	return c
}

// SendSMS requests a login code. It is unauthenticated and does not retry: a
// resend is the user's explicit action (§3.3).
func (c *Client) SendSMS(ctx context.Context, req SendSMSRequest) (*SendSMSData, error) {
	if req.Purpose == "" {
		req.Purpose = PurposeLogin
	}
	var out SendSMSData
	if err := c.do(ctx, http.MethodPost, pathSendSMS, req, &out, "", req.ClientRequestID); err != nil {
		return nil, err
	}
	return &out, nil
}

// Login signs in, activating on first use, and stores the returned credentials.
// Storing them here is what makes the activation and later-login paths one call
// from the caller's point of view.
func (c *Client) Login(ctx context.Context, req LoginRequest) (*LoginData, error) {
	var out LoginData
	if err := c.do(ctx, http.MethodPost, pathLogin, req, &out, "", req.ClientRequestID); err != nil {
		return nil, err
	}
	c.creds.Set(Credentials{
		AccessToken:  out.AccessToken,
		RefreshToken: out.RefreshToken,
		ExpiresAt:    c.now().Add(time.Duration(out.AccessTokenExpiresInSec) * time.Second),
	})
	return &out, nil
}

// Refresh exchanges a refresh token for a rotated pair. It does not touch the
// holder: refreshSingleFlight owns that, so a refresh can never half-apply.
func (c *Client) Refresh(ctx context.Context, refreshToken string) (*RefreshData, error) {
	var out RefreshData
	if err := c.do(ctx, http.MethodPost, pathRefresh, RefreshRequest{RefreshToken: refreshToken}, &out, "", ""); err != nil {
		return nil, err
	}
	return &out, nil
}

// Bootstrap fetches the account summary. It is the authenticated call the app
// makes at startup, so it is also the call that exercises token refresh.
func (c *Client) Bootstrap(ctx context.Context) (*BootstrapData, error) {
	var out BootstrapData
	if err := c.doAuthorized(ctx, http.MethodGet, pathBootstrap, nil, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

// EnsureToken makes sure an access token is held, exchanging the held refresh
// token for one when memory has none.
//
// WHY IT EXISTS. A process that has just started holds a refresh token and
// nothing else (需求基线 E6 规则 2: the access token is never written down), and
// every call that goes through doAuthorized repairs that by itself - it runs
// without a bearer, takes the 401 and refreshes. The gateway turn does not: it
// is an ordinary OpenAI-protocol request built by internal/provider/openai with
// the bearer read straight from the holder, so with no token it can only be
// refused. That refusal is correct and fail-closed; what was missing is anyone
// making the token exist first (V-39).
//
// It is a wrapper around refreshSingleFlight rather than a second exchange path,
// and deliberately so: the single-flight owns the rotation, the "never
// half-apply" rule and the concurrency, and it already treats "no access token
// held" as its cue (its early return requires a token that differs from the
// stale one). A second implementation would be a second thing to keep in step
// (开发规范 §3.5/§3.8).
//
// No file is touched. The rotated refresh token is the caller's to persist - the
// credential store owns that file, and a client that wrote it would be a second
// owner of data/credential.json.
//
// A refusal clears the held credential, exactly as doAuthorized does: C12/L-A6
// say an unauthorised answer ends the session, and it must end it in memory and
// on disk together rather than one layer at a time.
func (c *Client) EnsureToken(ctx context.Context) error {
	if c.creds.AccessToken() != "" {
		return nil
	}
	err := c.refreshSingleFlight(ctx, "")
	if err != nil && errors.Is(err, ErrSessionExpired) {
		c.creds.Clear()
	}
	return err
}

// doAuthorized performs an authenticated call with exactly one refresh and one
// replay when the access token is rejected (§C2 规则 4/5).
func (c *Client) doAuthorized(ctx context.Context, method, path string, body, out any) error {
	stale := c.creds.AccessToken()
	err := c.do(ctx, method, path, body, out, stale, "")
	if err == nil {
		return nil
	}
	if !isUnauthorized(err) {
		return err
	}

	if rerr := c.refreshSingleFlight(ctx, stale); rerr != nil {
		// A refresh token the platform refuses does not heal by retrying: the
		// session is over, the credential goes with it, and the caller lands on
		// the blocked screen (C12/L-A6).
		//
		// Any other failure is not that answer. An exchange that never completed
		// — a dropped connection, a 5xx, a timeout — has no verdict in it, and
		// clearing on one would turn a flaky network into a forced SMS login
		// (V-43: this used to be what happened, because refreshSingleFlight
		// reported every failure as an expired session).
		if errors.Is(rerr, ErrSessionExpired) {
			c.creds.Clear()
		}
		return rerr
	}

	if rerr := c.do(ctx, method, path, body, out, c.creds.AccessToken(), ""); rerr != nil {
		if isUnauthorized(rerr) {
			c.creds.Clear()
		}
		return rerr
	}
	return nil
}

// refreshSingleFlight collapses concurrent refreshes into one. stale is the
// access token the caller's failed request used: if the holder has already moved
// past it, another caller refreshed and there is nothing left to do.
func (c *Client) refreshSingleFlight(ctx context.Context, stale string) error {
	c.refreshMu.Lock()
	defer c.refreshMu.Unlock()

	if cur := c.creds.AccessToken(); cur != "" && cur != stale {
		return nil
	}
	token := c.creds.Get().RefreshToken
	if token == "" {
		return ErrSessionExpired
	}
	data, err := c.Refresh(ctx, token)
	if err != nil {
		if isUnauthorized(err) {
			return fmt.Errorf("%w: %w", ErrSessionExpired, err)
		}
		// Not a refusal, so not an answer about the session at all: the exchange
		// never completed. Returning ErrSessionExpired here is what let a
		// dropped connection end a live session (V-43), and dto.go's own
		// definition says otherwise — "the refresh token is gone or the platform
		// refused it". Every caller keys on that sentinel, so the distinction has
		// to be made here or it cannot be made later.
		return err
	}
	c.creds.Set(Credentials{
		AccessToken:  data.AccessToken,
		RefreshToken: data.RefreshToken,
		ExpiresAt:    c.now().Add(time.Duration(data.AccessTokenExpiresInSec) * time.Second),
	})
	return nil
}

// do performs one HTTP round trip and decodes either envelope.
func (c *Client) do(ctx context.Context, method, path string, body, out any, bearer, idempotencyKey string) error {
	var reader io.Reader
	if body != nil {
		buf, err := json.Marshal(body)
		if err != nil {
			return fmt.Errorf("productclient: encode %s: %w", path, err)
		}
		reader = bytes.NewReader(buf)
	}

	req, err := http.NewRequestWithContext(ctx, method, c.baseURL+path, reader)
	if err != nil {
		return fmt.Errorf("productclient: build %s: %w", path, err)
	}
	req.Header.Set("Accept", "application/json")
	if body != nil {
		req.Header.Set("Content-Type", "application/json; charset=utf-8")
	}
	if c.meta.Version != "" {
		req.Header.Set(HeaderClientVersion, c.meta.Version)
	}
	if c.meta.Platform != "" {
		req.Header.Set(HeaderClientPlatform, c.meta.Platform)
	}
	if c.meta.Arch != "" {
		req.Header.Set(HeaderClientArch, c.meta.Arch)
	}
	if c.meta.InstallID != "" {
		req.Header.Set(HeaderInstallID, c.meta.InstallID)
	}
	if idempotencyKey != "" {
		req.Header.Set(HeaderIdempotencyKey, idempotencyKey)
	}
	if bearer != "" {
		req.Header.Set("Authorization", "Bearer "+bearer)
	}

	resp, err := c.http.Do(req)
	if err != nil {
		// The request never reached the platform. That is a different fact from
		// a 5xx the platform itself returned, and the registered code says so.
		return &Error{Code: CodeNetworkUnavailable, Message: err.Error()}
	}
	defer func() { _ = resp.Body.Close() }()

	raw, err := io.ReadAll(io.LimitReader(resp.Body, maxBody))
	if err != nil {
		return &Error{Code: CodeNetworkUnavailable, Message: err.Error()}
	}
	if resp.StatusCode < 200 || resp.StatusCode > 299 {
		return decodeError(resp.StatusCode, raw)
	}
	if out == nil {
		return nil
	}

	var env struct {
		Data json.RawMessage `json:"data"`
	}
	if err := json.Unmarshal(raw, &env); err != nil {
		return fmt.Errorf("productclient: decode %s: %w", path, err)
	}
	if err := json.Unmarshal(env.Data, out); err != nil {
		return fmt.Errorf("productclient: decode %s data: %w", path, err)
	}
	return nil
}

// decodeError turns a failure body into an *Error. A body without a code is
// still a failure: the status is preserved, and the code stays generic rather
// than inventing a business value the platform did not send.
func decodeError(status int, raw []byte) error {
	var payload struct {
		Code          string `json:"code"`
		Message       string `json:"message"`
		Field         string `json:"field"`
		RetryAfterSec int    `json:"retryAfterSec"`
		RequestID     string `json:"requestId"`
		PhoneMasked   string `json:"phoneMasked"`
	}
	if err := json.Unmarshal(raw, &payload); err != nil || payload.Code == "" {
		code := CodeInternalError
		if status >= 500 {
			// Something in front of, or behind, the platform failed without
			// speaking the envelope. Saying upstream_unavailable is accurate;
			// claiming internal_error would blame the wrong side.
			code = CodeUpstreamUnavailable
		}
		return &Error{Code: code, Status: status}
	}
	return &Error{
		Code:          payload.Code,
		Message:       payload.Message,
		Field:         payload.Field,
		RetryAfterSec: payload.RetryAfterSec,
		RequestID:     payload.RequestID,
		PhoneMasked:   payload.PhoneMasked,
		Status:        status,
	}
}

// isUnauthorized reports whether err is a rejected access token. The platform
// may answer 401 with either registered code, so the code is checked too.
func isUnauthorized(err error) bool {
	var ae *Error
	if !errors.As(err, &ae) {
		return false
	}
	return ae.Status == http.StatusUnauthorized ||
		ae.Code == CodeUnauthorized ||
		ae.Code == CodeTokenExpired
}

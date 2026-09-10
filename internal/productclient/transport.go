package productclient

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"
)

// Transport is the single place this package speaks HTTP. It implements
// P0-01 §4's "一个共享 HTTP transport，不是每个 client 一套": the common headers,
// the per-endpoint timeout table, the status→code mapping, the 401 refresh
// single-flight and the Idempotency-Key rule each exist once, not once per
// method. AuthClient, ControlPlaneClient, UsageClient and gateway's control
// calls all share one instance.
//
// The split matters for a second reason: `reuse-guard` bans net/http in
// internal/productclient/gateway, because the *model stream* must reuse
// internal/app's provider stack. The control plane is ordinary JSON and has no
// upstream equivalent to reuse, so it lives here on purpose.
type Transport struct {
	base      *url.URL
	client    *http.Client
	version   string
	installID string
	locale    string
	platform  string
	arch      string

	// tokens is nil for calls made before a session exists (SendCode,
	// Login). It is an interface so the credential store, the token holder and
	// the refresh policy stay outside this package.
	tokens TokenSource

	sleep func(context.Context, time.Duration) error

	refresh singleFlight
}

// TokenSource is the transport's view of the product session. It is deliberately
// two-way: the transport reads a token and reports unrecoverable auth loss, but
// it does not decide how tokens are stored, rotated or discarded — that is
// P0-02's credential handling and P0-08's platform store.
type TokenSource interface {
	// AccessToken is the current bearer token, or "" when there is none.
	AccessToken() string
	// Refresh exchanges the stored refresh token for a new access token. The
	// transport calls it at most once per burst of 401s, so an implementation
	// may assume it is not being stampeded by concurrent callers — but it must
	// still be safe to call twice in a row (a second expiry later in the
	// session is normal).
	Refresh(ctx context.Context) (string, error)
	// AuthLost reports that the session cannot be recovered. After this the
	// transport stops attaching a token, so the caller must route the user back
	// to the login page. It is called once per unrecoverable failure.
	AuthLost(err error)
}

// CallKind selects the timeout and retry policy, and is the only thing a caller
// has to get right to inherit 01 §4's table. The distinction between CallWrite
// and the other two is the load-bearing one: an automatic retry of SendCode
// means a second SMS the user did not ask for, and an automatic retry of Login
// means a second activation attempt.
type CallKind int

const (
	// CallWrite creates a side effect and is never automatically retried:
	// SendCode, Login, Logout, AcceptLegal.
	CallWrite CallKind = iota
	// CallControl is the signed-envelope group: Bootstrap, Models,
	// SensitiveDictionary, LegalDocuments. It is also the ETag group.
	CallControl
	// CallRead is read-only and therefore safely repeatable: Usage,
	// RequestStatus, Ledger, Cancel (idempotent by contract).
	CallRead
)

const (
	timeoutWrite   = 10 * time.Second
	timeoutControl = 15 * time.Second
	timeoutRead    = 10 * time.Second

	// maxAttempts is the total number of sends, not the number of retries.
	maxAttemptsWrite   = 1 // no automatic retry at all
	maxAttemptsControl = 3 // initial + 2
	maxAttemptsRead    = 3

	// backoffBase/backoffMax/maxTotalBackoff are the "明确数值" 01 §4 requires.
	// Without a total cap a chain of Retry-After headers can pin a request open
	// indefinitely, which is indistinguishable from a hang.
	backoffBase     = 500 * time.Millisecond
	backoffMax      = 5 * time.Second
	maxTotalBackoff = 15 * time.Second

	// backoffCeiling is the largest server-mandated Retry-After this transport
	// will honour. A platform that answers "come back in an hour" is telling us
	// to stop, not to sleep: sleeping would hold the user's request open and
	// still fail. The caller gets the error and its RetryAfter instead.
	backoffCeiling = 60 * time.Second

	// maxResponseBytes bounds a JSON response. A catalog is not tiny, but an
	// unbounded ReadAll against a hostile or broken endpoint is a memory
	// exhaustion primitive. A response at the cap is reported as malformed
	// rather than silently truncated, because a truncated envelope is exactly
	// what a signature check must never be handed.
	maxResponseBytes = 8 << 20
)

// ErrNotModified is returned when a control-plane GET answered 304, meaning the
// caller's ETag is still current and its cache should be used as-is. It is not
// an error condition and must not be shown to the user.
var ErrNotModified = errors.New("productclient: resource not modified")

// TransportConfig is what the assembly root must supply. Everything here is
// deliberately explicit: there are no defaults for the identity fields, because
// a client that silently sends an empty X-Install-Id or X-Client-Version loses
// per-install rate limiting and makes support tickets unattributable.
type TransportConfig struct {
	// BaseURL is the control-plane host. In production it comes from the
	// embedded profile, never from config.yml or the environment (P0-01 §4.1).
	BaseURL string

	// ClientVersion, InstallID, Locale, Platform, Arch fill the common headers.
	// InstallID must be a random per-install identifier; it must not be derived
	// from a MAC address or disk serial (中台交付包 §4.2). Generating and
	// persisting it is the assembly root's job, not this package's: a leaf
	// package must not resolve data paths (开发规范 §3.1).
	ClientVersion string
	InstallID     string
	Locale        string
	Platform      string
	Arch          string

	// HTTPClient is reused for connection pooling. A nil one is built with a
	// sane pool; the per-call deadline comes from the timeout table, not from
	// this client, so a caller who sets Timeout here cannot accidentally
	// override the table.
	HTTPClient *http.Client

	// Tokens enables Authorization and the 401 single-flight. Nil means an
	// unauthenticated client (SendCode/Login) that treats 401 as final.
	Tokens TokenSource

	// AllowInsecure permits an http:// BaseURL. It exists for the developer
	// profile, local sandbox and httptest; a production profile must leave it
	// false. Whether a build is allowed to set it is the profile's decision,
	// not this package's — productclient cannot import productprofile
	// (internal/productruntime/deps_test.go), so the flag is passed in.
	AllowInsecure bool

	// Sleep is injectable so tests exercise the backoff schedule without
	// waiting for it. Nil means time-based sleeping.
	Sleep func(context.Context, time.Duration) error
}

// NewTransport validates the configuration and returns the shared client.
func NewTransport(cfg TransportConfig) (*Transport, error) {
	u, err := parseBaseURL(cfg.BaseURL, cfg.AllowInsecure)
	if err != nil {
		return nil, err
	}
	var missing []string
	for _, f := range []struct {
		name, value string
	}{
		{"ClientVersion", cfg.ClientVersion},
		{"InstallID", cfg.InstallID},
	} {
		if strings.TrimSpace(f.value) == "" {
			missing = append(missing, f.name)
		}
	}
	if len(missing) > 0 {
		return nil, fmt.Errorf("productclient: transport is missing %s", strings.Join(missing, ", "))
	}

	hc := cfg.HTTPClient
	if hc == nil {
		hc = &http.Client{
			Transport: &http.Transport{
				MaxIdleConns:        32,
				MaxIdleConnsPerHost: 8,
				IdleConnTimeout:     90 * time.Second,
			},
			// No Timeout here on purpose: the deadline is per call and comes
			// from the timeout table. A client-level timeout would silently
			// shorten CallControl's 15s.
		}
	}
	sleep := cfg.Sleep
	if sleep == nil {
		sleep = sleepContext
	}
	return &Transport{
		base:      u,
		client:    hc,
		version:   cfg.ClientVersion,
		installID: cfg.InstallID,
		locale:    cfg.Locale,
		platform:  cfg.Platform,
		arch:      cfg.Arch,
		tokens:    cfg.Tokens,
		sleep:     sleep,
	}, nil
}

func parseBaseURL(raw string, allowInsecure bool) (*url.URL, error) {
	if strings.TrimSpace(raw) == "" {
		return nil, errors.New("productclient: transport has no base URL")
	}
	u, err := url.Parse(raw)
	if err != nil {
		return nil, fmt.Errorf("productclient: malformed base URL: %w", err)
	}
	if u.Host == "" || !u.IsAbs() {
		return nil, fmt.Errorf("productclient: base URL %q is not absolute", raw)
	}
	switch u.Scheme {
	case "https":
	case "http":
		// Plaintext is not merely discouraged: 中台交付包 §3.1 requires TLS, and
		// a plaintext control plane leaks the bearer token and the phone number
		// to anything on the path. The escape hatch is explicit and auditable.
		if !allowInsecure {
			return nil, fmt.Errorf("productclient: base URL %q is plaintext http; set AllowInsecure only for developer/test builds", raw)
		}
	default:
		return nil, fmt.Errorf("productclient: base URL scheme %q is not supported", u.Scheme)
	}
	if u.User != nil {
		return nil, errors.New("productclient: base URL must not embed credentials")
	}
	return u, nil
}

// Request is one control-plane call.
type Request struct {
	// Op names the caller for diagnostics, e.g. "Login". Defaults to
	// "METHOD path".
	Op string
	// Method is an HTTP method. Path is appended to the base URL.
	Method string
	Path   string
	Query  url.Values
	// Body is JSON-marshalled when non-nil.
	Body any
	Kind CallKind
	// ClientRequestID, when set, is sent as Idempotency-Key. Set it for any
	// call that creates a side effect (中台交付包 §3.3). It is the same value
	// that identifies the call in the ledger, so it is the caller's to choose.
	ClientRequestID string
	// ETag, when set, is sent as If-None-Match. A 304 answer surfaces as
	// ErrNotModified.
	ETag string
	// Out receives the decoded `data` object; nil for calls with no payload.
	Out any
	// SkipAuthRefresh marks a request as part of credential recovery itself, so
	// a 401 must be answered as a final failure instead of triggering the
	// refresh-and-replay below.
	//
	// Exactly one caller sets it: AuthClient.Refresh, whose whole job is to
	// decide whether the stored refresh token is still good. Without it a 401
	// from the refresh endpoint is catastrophic rather than merely terminal —
	// the recovery call re-enters the same single-flight it is being run from
	// and waits for itself, so the call never returns and the goroutine never
	// exits. That is the normal shape of a *revoked* session
	// (中台交付包 §4.2.4: "客户端下次 refresh 必须失败并清理本地凭证"), not an
	// edge case.
	SkipAuthRefresh bool
}

func (r Request) op() string {
	if r.Op != "" {
		return r.Op
	}
	return strings.TrimSpace(r.Method + " " + r.Path)
}

func timeoutFor(kind CallKind) time.Duration {
	switch kind {
	case CallControl:
		return timeoutControl
	case CallRead:
		return timeoutRead
	default:
		return timeoutWrite
	}
}

func maxAttemptsFor(kind CallKind) int {
	switch kind {
	case CallControl:
		return maxAttemptsControl
	case CallRead:
		return maxAttemptsRead
	default:
		return maxAttemptsWrite
	}
}

// InstallID returns the per-install identifier the transport sends as
// X-Install-Id. AuthClient needs it because the platform's login body carries
// the same value (中台交付包 §4.2.2), and reading it from the transport keeps it
// from being configured twice into disagreeing values.
func (t *Transport) InstallID() string { return t.installID }

// Do performs one logical call, including any permitted retries.
func (t *Transport) Do(ctx context.Context, req Request) error {
	body, err := encodeBody(&req)
	if err != nil {
		return LocalError(req.op(), CodeInvalidRequest, "cannot encode request body: %v", err)
	}

	attempts := maxAttemptsFor(req.Kind)
	timeout := timeoutFor(req.Kind)

	// refreshed is shared by every attempt of this Do: a 401 may only consume
	// one refresh, so a server that keeps answering 401 cannot drain a rotating
	// refresh token.
	var refreshed bool
	var slept time.Duration
	var last error

	for attempt := 0; attempt < attempts; attempt++ {
		if attempt > 0 {
			wait, ok := t.backoffFor(last, attempt)
			if !ok || slept+wait > maxTotalBackoff {
				return last
			}
			if err := t.sleep(ctx, wait); err != nil {
				return last
			}
			slept += wait
		}
		retry, err := t.attempt(ctx, req, body, timeout, &refreshed)
		if err == nil {
			return nil
		}
		last = err
		if !retry {
			return err
		}
		if ctx.Err() != nil {
			// The caller has given up. Retrying would keep working behind their
			// back, and every additional attempt would be logged as a platform
			// problem the user did not experience.
			return err
		}
	}
	return last
}

// attempt performs one logical attempt, which may involve at most one 401
// replay. The boolean reports whether the retry loop is permitted to try again.
func (t *Transport) attempt(ctx context.Context, req Request, body []byte, timeout time.Duration, refreshed *bool) (bool, error) {
	op := req.op()

	for {
		status, payload, err := t.send(ctx, req, body, timeout)
		if err != nil {
			var pe *Error
			if errors.As(err, &pe) {
				return pe.Retryable(), pe
			}
			e := networkError(op, err)
			return e.Retryable(), e
		}

		if status == http.StatusUnauthorized && t.tokens != nil && !req.SkipAuthRefresh {
			if *refreshed {
				// A 401 *after* a successful refresh means the new token is also
				// refused. Retrying would loop, and refreshing again would burn
				// another slot of a rotating token.
				return false, t.authLost(op, errors.New("platform refused a freshly refreshed token"))
			}
			*refreshed = true
			if err := t.renewToken(ctx); err != nil {
				return false, t.authLost(op, err)
			}
			// Replay with the new credential. This consumes neither a retry nor
			// a backoff slot — and it lives inside attempt rather than in the
			// retry loop on purpose: the loop runs once for CallWrite, so a 401
			// on Logout could otherwise never be refreshed.
			continue
		}

		if status == http.StatusNotModified {
			// 304 carries no envelope, so DecodeEnvelope would call it malformed.
			return false, ErrNotModified
		}

		err = DecodeEnvelope(op, status, payload, req.Out)
		if err == nil {
			return false, nil
		}
		var pe *Error
		if errors.As(err, &pe) {
			return pe.Retryable(), pe
		}
		return false, err
	}
}

// send is one raw round trip. It returns the status and body without
// interpreting either, so the semantics (401 replay, 304, error mapping) stay in
// one caller.
func (t *Transport) send(ctx context.Context, req Request, body []byte, timeout time.Duration) (int, []byte, error) {
	httpReq, cancel, err := t.newRequest(ctx, req, body, timeout)
	if err != nil {
		return 0, nil, err
	}
	defer cancel()

	resp, err := t.client.Do(httpReq)
	if err != nil {
		return 0, nil, err
	}
	defer resp.Body.Close()

	payload, err := io.ReadAll(io.LimitReader(resp.Body, maxResponseBytes))
	if err != nil {
		return resp.StatusCode, nil, err
	}
	return resp.StatusCode, payload, nil
}

func (t *Transport) newRequest(ctx context.Context, req Request, body []byte, timeout time.Duration) (*http.Request, context.CancelFunc, error) {
	u := *t.base
	u.Path = strings.TrimSuffix(t.base.Path, "/") + req.Path
	if len(req.Query) > 0 {
		u.RawQuery = req.Query.Encode()
	}

	ctx, cancel := context.WithTimeout(ctx, timeout)

	var rdr io.Reader
	if body != nil {
		rdr = bytes.NewReader(body)
	}
	httpReq, err := http.NewRequestWithContext(ctx, req.Method, u.String(), rdr)
	if err != nil {
		cancel()
		return nil, nil, &Error{Op: req.op(), Code: CodeInvalidRequest, Message: "cannot build request", Err: err}
	}

	// Common headers are set here and nowhere else (01 §4.2). A method that
	// sets its own X-Client-Version is how two versions end up in one build.
	h := httpReq.Header
	h.Set("Accept", "application/json")
	h.Set("X-Client-Version", t.version)
	h.Set("X-Install-Id", t.installID)
	if t.platform != "" {
		h.Set("X-Client-Platform", t.platform)
	}
	if t.arch != "" {
		h.Set("X-Client-Arch", t.arch)
	}
	if t.locale != "" {
		h.Set("Accept-Language", t.locale)
	}
	if body != nil {
		h.Set("Content-Type", "application/json")
	}
	if t.tokens != nil {
		if tok := t.tokens.AccessToken(); tok != "" {
			h.Set("Authorization", "Bearer "+tok)
		}
	}
	if req.ClientRequestID != "" {
		h.Set("Idempotency-Key", req.ClientRequestID)
	}
	if req.ETag != "" {
		h.Set("If-None-Match", req.ETag)
	}
	return httpReq, cancel, nil
}

// renewToken refreshes through the single-flight, so N simultaneous 401s cause
// one rotation.
func (t *Transport) renewToken(ctx context.Context) error {
	_, err := t.refresh.Do(ctx, func(c context.Context) (string, error) {
		return t.tokens.Refresh(c)
	})
	return err
}

// authLost records unrecoverable auth failure exactly once and returns the error
// the caller sees.
func (t *Transport) authLost(op string, cause error) error {
	t.tokens.AuthLost(cause)
	return &Error{
		Op:         op,
		Code:       CodeUnauthorized,
		HTTPStatus: http.StatusUnauthorized,
		Message:    "session cannot be renewed",
		Err:        cause,
	}
}

// backoffFor returns the wait before the next attempt, and whether waiting is
// permitted at all.
func (t *Transport) backoffFor(last error, attempt int) (time.Duration, bool) {
	var pe *Error
	if !errors.As(last, &pe) {
		return 0, false
	}
	if d, ok := pe.RetryAfter(); ok {
		if d > backoffCeiling {
			// The platform named a cooldown longer than a user will wait. Sleep
			// is not the answer; reporting the cooldown is.
			return 0, false
		}
		return d, true
	}
	d := backoffBase << (attempt - 1)
	if d > backoffMax {
		d = backoffMax
	}
	return d, true
}

func encodeBody(req *Request) ([]byte, error) {
	if req.Body == nil {
		return nil, nil
	}
	return json.Marshal(req.Body)
}

// networkError classifies a transport-level failure. A cancelled context is the
// caller's own doing and must not be reported as a network outage — otherwise
// every user-initiated cancel looks like a platform incident in the logs.
func networkError(op string, err error) *Error {
	if errors.Is(err, context.Canceled) {
		return &Error{Op: op, Code: CodeNetworkUnavailable, Message: "request cancelled", Err: err}
	}
	return &Error{Op: op, Code: CodeNetworkUnavailable, Message: "request did not reach the platform", Err: err}
}

func sleepContext(ctx context.Context, d time.Duration) error {
	timer := time.NewTimer(d)
	defer timer.Stop()
	select {
	case <-timer.C:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

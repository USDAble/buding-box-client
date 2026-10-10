package productruntime

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"time"

	"github.com/open-octo/octo-agent/internal/productclient"
)

// gatewayAuthTransport retries only a rejected HTTP handshake, never an opened
// stream. The same transport serves foreground, cron and background agents.
type gatewayAuthTransport struct {
	base       http.RoundTripper
	tokens     *productclient.CredentialHolder
	ensure     func(context.Context) error
	renew      func(context.Context, string) error
	generation uint64
	origin     string
}

func (t *gatewayAuthTransport) RoundTrip(req *http.Request) (response *http.Response, failure error) {
	defer func() {
		if failure != nil {
			failure = gatewayAuthError(failure)
		}
	}()
	if req.URL.Scheme+"://"+req.URL.Host != t.origin {
		return nil, fmt.Errorf("gateway authentication cannot follow a cross-origin redirect")
	}
	if err := t.checkSession(req.Context()); err != nil {
		return nil, err
	}
	if t.ensure != nil {
		if err := gatewayRenewal(req.Context(), t.ensure); err != nil {
			return nil, err
		}
	}
	if err := t.checkSession(req.Context()); err != nil {
		return nil, err
	}
	stale, sameSession := t.tokens.AccessTokenForSession(t.generation)
	if !sameSession {
		return nil, productclient.ErrSessionChanged
	}
	if stale == "" {
		return nil, productclient.ErrSessionExpired
	}
	attempt := req.Clone(req.Context())
	attempt.Header.Set("Authorization", "Bearer "+stale)
	resp, err := t.base.RoundTrip(attempt)
	if err != nil || resp.StatusCode != http.StatusUnauthorized || t.renew == nil {
		return resp, err
	}
	// The provider's JSON request has GetBody; without it, replay would send an
	// empty body. Preserve the refusal rather than inventing a second request.
	if req.GetBody == nil {
		return resp, nil
	}
	resp.Body.Close()
	if err := t.checkSession(req.Context()); err != nil {
		return nil, err
	}
	slog.Info("gateway: renewing a rejected access token")
	err = gatewayRenewal(req.Context(), func(ctx context.Context) error { return t.renew(ctx, stale) })
	if err != nil {
		return nil, err
	}
	if err := t.checkSession(req.Context()); err != nil {
		return nil, err
	}
	token, sameSession := t.tokens.AccessTokenForSession(t.generation)
	if !sameSession {
		return nil, productclient.ErrSessionChanged
	}
	if token == "" {
		return nil, productclient.ErrSessionExpired
	}
	attempt = req.Clone(req.Context())
	attempt.Body, err = req.GetBody()
	if err != nil {
		return nil, err
	}
	attempt.Header.Set("Authorization", "Bearer "+token)
	// Exactly one replay, with the original body and task/request headers.
	return t.base.RoundTrip(attempt)
}

type gatewayAuthFailure struct {
	cause error
	code  string
}

func (e *gatewayAuthFailure) Error() string     { return e.cause.Error() }
func (e *gatewayAuthFailure) Unwrap() error     { return e.cause }
func (e *gatewayAuthFailure) ErrorCode() string { return e.code }

func gatewayAuthError(err error) error {
	code := ""
	var platform *productclient.Error
	switch {
	case errors.Is(err, productclient.ErrSessionExpired):
		code = productclient.CodeUnauthorized
	case errors.Is(err, productclient.ErrSessionChanged):
		code = "CLIENT_TOKEN_INVALID"
	case errors.As(err, &platform):
		code = platform.Code
	case errors.Is(err, context.DeadlineExceeded):
		code = productclient.CodeNetworkUnavailable
	}
	return &gatewayAuthFailure{cause: err, code: code}
}

// Only the credential exchange retries temporary outages. Provider failures and
// successful SSE responses remain owned by the existing provider/agent layers.
func gatewayRenewal(parent context.Context, renew func(context.Context) error) error {
	ctx, cancel := context.WithTimeout(parent, gatewayEnsureBudget)
	defer cancel()
	for attempt := 0; ; attempt++ {
		err := renew(ctx)
		if ctx.Err() != nil {
			return ctx.Err()
		}
		var platform *productclient.Error
		if attempt == 1 || !errors.As(err, &platform) ||
			(platform.Code != productclient.CodeNetworkUnavailable && platform.Status < 500) {
			return err
		}
		slog.Warn("gateway: credential exchange temporarily unavailable; retrying once")
		timer := time.NewTimer(250 * time.Millisecond)
		select {
		case <-ctx.Done():
			timer.Stop()
			return ctx.Err()
		case <-timer.C:
		}
	}
}

func (t *gatewayAuthTransport) checkSession(ctx context.Context) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if t.tokens.SessionGeneration() != t.generation {
		return productclient.ErrSessionChanged
	}
	return nil
}

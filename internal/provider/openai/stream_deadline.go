package openai

// OCTO-FORK: a healthy long generation must not share the stalled handshake/tail budget.
import (
	"context"
	"fmt"
	"io"
	"net/http"
	"sync/atomic"
	"time"
)

func streamPhaseTimeout(value, fallback time.Duration) time.Duration {
	if value == 0 {
		return fallback
	}
	return value
}

type streamCancelBody struct {
	io.ReadCloser
	cancel context.CancelFunc
}

func (b *streamCancelBody) Close() error {
	b.cancel()
	return b.ReadCloser.Close()
}

func (c *Client) openStream(req *http.Request) (*http.Response, error) {
	ctx, cancel := context.WithCancel(req.Context())
	var timedOut atomic.Bool
	var timer *time.Timer
	if d := streamPhaseTimeout(c.StreamHeaderTimeout, time.Minute); d > 0 {
		timer = time.AfterFunc(d, func() { timedOut.Store(true); cancel() })
	}
	resp, err := c.streamingHTTPClient().Do(req.Clone(ctx))
	if timer != nil && !timer.Stop() {
		// Stop(false) can race the callback; cancellation is already committed.
		if resp != nil {
			resp.Body.Close()
		}
		cancel()
		if req.Context().Err() != nil {
			return nil, req.Context().Err()
		}
		return nil, fmt.Errorf("openai: response headers timed out: %w", context.DeadlineExceeded)
	}
	if err != nil {
		cancel()
		if timedOut.Load() {
			return nil, fmt.Errorf("openai: response headers timed out: %w", context.DeadlineExceeded)
		}
		return nil, err
	}
	resp.Body = &streamCancelBody{ReadCloser: resp.Body, cancel: cancel}
	return resp, nil
}

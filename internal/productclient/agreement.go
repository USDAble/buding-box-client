package productclient

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
)

// Agreement is the currently published, plain-text platform agreement.
type Agreement struct {
	Kind        string `json:"kind"`
	Version     string `json:"version"`
	Title       string `json:"title"`
	Content     string `json:"content"`
	PublishedAt string `json:"published_at"`
}

// Agreement reads the public agreement without sending credentials or refreshing
// a session. Each call fetches the latest publication.
func (c *Client) Agreement(ctx context.Context, kind string) (*Agreement, error) {
	if kind != "box" && kind != "privacy" {
		return nil, fmt.Errorf("invalid agreement kind")
	}
	// OCTO-FORK: the portable client has one agreement contract, code="OK".
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, strings.TrimRight(c.baseURL, "/")+"/client/agreements/"+kind, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Accept", "application/json")
	req.Header.Set("Cache-Control", "no-cache")
	resp, err := c.http.Do(req)
	if err != nil {
		return nil, &Error{Code: CodeNetworkUnavailable}
	}
	defer resp.Body.Close()
	raw, err := io.ReadAll(io.LimitReader(resp.Body, maxBody+1))
	if err != nil || len(raw) > maxBody {
		return nil, fmt.Errorf("invalid agreement response body")
	}
	if resp.StatusCode != http.StatusOK {
		return nil, decodeError(resp.StatusCode, raw)
	}
	var envelope struct {
		Code string    `json:"code"`
		Data Agreement `json:"data"`
	}
	if err := json.Unmarshal(raw, &envelope); err != nil {
		return nil, fmt.Errorf("decode agreement: %w", err)
	}
	a := envelope.Data
	if envelope.Code != "OK" || a.Kind != kind || strings.TrimSpace(a.Version) == "" || strings.TrimSpace(a.Title) == "" || strings.TrimSpace(a.Content) == "" {
		return nil, fmt.Errorf("invalid agreement response")
	}
	return &a, nil
}

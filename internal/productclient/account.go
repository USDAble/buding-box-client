package productclient

import (
	"context"
	"fmt"
	"net/http"
	"strings"
)

type AccountNickname struct {
	Nickname string `json:"nickname"`
}

// UpdateNickname persists the account edit before the caller updates its local projection.
func (c *Client) UpdateNickname(ctx context.Context, nickname string) (*AccountNickname, error) {
	var out AccountNickname
	if err := c.doAuthorized(ctx, http.MethodPut, "/client/account/nickname", AccountNickname{Nickname: nickname}, &out); err != nil {
		return nil, err
	}
	if strings.TrimSpace(out.Nickname) == "" {
		return nil, fmt.Errorf("productclient: incomplete nickname response")
	}
	return &out, nil
}

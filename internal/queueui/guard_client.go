package queueui

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"

	"github.com/avery20024/herdr-async-message-relay/internal/claudeguard"
	"github.com/avery20024/herdr-async-message-relay/internal/deliver"
)

type GuardClient interface {
	GuardSettings(context.Context) (deliver.GuardSettings, error)
	SaveGuard(context.Context, bool) (deliver.GuardSettings, error)
}

func (c *Client) GuardSettings(ctx context.Context) (deliver.GuardSettings, error) {
	return c.guardRequest(ctx, http.MethodGet, nil)
}
func (c *Client) SaveGuard(ctx context.Context, enabled bool) (deliver.GuardSettings, error) {
	body, err := json.Marshal(claudeguard.Policy{Version: 1, Enabled: enabled})
	if err != nil {
		return deliver.GuardSettings{}, err
	}
	return c.guardRequest(ctx, http.MethodPut, body)
}
func (c *Client) guardRequest(ctx context.Context, method string, body []byte) (deliver.GuardSettings, error) {
	var result deliver.GuardSettings
	req, err := http.NewRequestWithContext(ctx, method, "http://unix/v1/claude-guard", bytes.NewReader(body))
	if err != nil {
		return result, err
	}
	if method == http.MethodPut {
		req.Header.Set("Content-Type", "application/json")
	}
	resp, err := c.http.Do(req)
	if err != nil {
		return result, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return result, fmt.Errorf("team guard HTTP %d; reload to confirm actual policy", resp.StatusCode)
	}
	data, err := io.ReadAll(io.LimitReader(resp.Body, 1024*1024+1))
	if err != nil {
		return result, err
	}
	if len(data) > 1024*1024 {
		return result, fmt.Errorf("team guard response too large")
	}
	if err = json.Unmarshal(data, &result); err != nil {
		return result, err
	}
	if result.ProjectRoot != c.root || result.Policy.Version != 1 || result.Observations == nil {
		return result, fmt.Errorf("invalid team guard response")
	}
	return result, nil
}

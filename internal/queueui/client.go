// Package queueui 提供不啟動服務、不寫入佇列的終端檢視介面。
package queueui

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"strconv"
	"time"

	"github.com/avery20024/herdr-async-message-relay/internal/deliver"
)

type Filter struct{ Sender, Recipient, State string }

type Loader interface {
	Page(context.Context, Filter, int64) (deliver.BrowseResponse, error)
}

type Client struct {
	root string
	http *http.Client
}

func NewClient(c deliver.Config) *Client {
	return &Client{root: c.Root, http: &http.Client{
		Timeout:       5 * time.Second,
		CheckRedirect: func(*http.Request, []*http.Request) error { return errors.New("unexpected relay redirect") },
		Transport: &http.Transport{DialContext: func(ctx context.Context, _, _ string) (net.Conn, error) {
			return (&net.Dialer{}).DialContext(ctx, "unix", c.Socket())
		}},
	}}
}
func (c *Client) Close() { c.http.CloseIdleConnections() }

func (c *Client) Page(ctx context.Context, f Filter, before int64) (deliver.BrowseResponse, error) {
	var result deliver.BrowseResponse
	query := url.Values{"limit": {"50"}, "sender": {f.Sender}, "recipient": {f.Recipient}, "state": {f.State}}
	if before > 0 {
		query.Set("before", strconv.FormatInt(before, 10))
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, "http://unix/v1/messages?"+query.Encode(), nil)
	if err != nil {
		return result, err
	}
	resp, err := c.http.Do(req)
	if err != nil {
		return result, err
	}
	defer resp.Body.Close()
	if resp.StatusCode == 404 || resp.StatusCode == 405 {
		return result, errors.New("worker does not support UI history API; upgrade separately")
	}
	if resp.StatusCode != 200 {
		return result, fmt.Errorf("relay HTTP %d", resp.StatusCode)
	}
	// 最多 50 筆，每筆本文受既有入列限制；額外保留 JSON escaping 空間。
	const maxResponse = 24 * 1024 * 1024
	body, err := io.ReadAll(io.LimitReader(resp.Body, maxResponse+1))
	if err != nil {
		return result, err
	}
	if len(body) > maxResponse {
		return result, errors.New("relay response too large")
	}
	if err := json.Unmarshal(body, &result); err != nil {
		return result, err
	}
	if result.ProjectRoot != c.root {
		return result, errors.New("relay project root does not match requested root")
	}
	if result.Items == nil || len(result.Items) > 50 || result.NextBefore < 0 {
		return result, errors.New("invalid history response")
	}
	var last int64
	for i, row := range result.Items {
		if row.ID == "" || row.Seq <= 0 || (i > 0 && row.Seq >= last) || (before > 0 && row.Seq >= before) {
			return result, errors.New("invalid history ordering")
		}
		if (f.Sender != "" && row.Sender != f.Sender) || (f.Recipient != "" && row.Recipient != f.Recipient) || (f.State != "" && row.State != f.State) {
			return result, errors.New("relay ignored history filters")
		}
		last = row.Seq
	}
	if result.NextBefore != 0 && (last == 0 || result.NextBefore != last) {
		return result, errors.New("invalid history cursor")
	}
	return result, nil
}

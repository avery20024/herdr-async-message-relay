package queueui

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"testing"

	"github.com/avery20024/herdr-async-message-relay/internal/deliver"
	"github.com/avery20024/herdr-async-message-relay/internal/sqlc"
)

type transportFunc func(*http.Request) (*http.Response, error)

func (f transportFunc) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }
func TestClientOnlyReadsAndValidates(t *testing.T) {
	for _, scenario := range []string{"ok", "legacy", "wrong-root", "ignored-filter", "bad-order", "bad-cursor", "null-items", "redirect", "http-error", "bad-json"} {
		t.Run(scenario, func(t *testing.T) {
			c := &Client{root: "/team", http: &http.Client{Transport: transportFunc(func(r *http.Request) (*http.Response, error) {
				if r.Method != "GET" || r.URL.Path != "/v1/messages" || r.URL.Query().Get("sender") != "dev & me" || r.URL.Query().Get("before") != "9" {
					t.Fatalf("unexpected request: %s %s", r.Method, r.URL)
				}
				result := deliver.BrowseResponse{ProjectRoot: "/team", Items: []sqlc.BrowseMessagesRow{{ID: "x", Seq: 8, Sender: "dev & me"}}}
				code := 200
				switch scenario {
				case "legacy":
					code = 404
				case "wrong-root":
					result.ProjectRoot = "/other"
				case "ignored-filter":
					result.Items[0].Sender = "other"
				case "bad-order":
					result.Items[0].Seq = 9
				case "bad-cursor":
					result.NextBefore = 7
				case "null-items":
					result.Items = nil
				case "redirect":
					code = 302
				case "http-error":
					code = 500
				}
				data, _ := json.Marshal(result)
				if scenario == "bad-json" {
					data = []byte("broken")
				}
				return &http.Response{StatusCode: code, Body: io.NopCloser(strings.NewReader(string(data))), Header: make(http.Header)}, nil
			})}}
			_, err := c.Page(t.Context(), Filter{Sender: "dev & me"}, 9)
			if (err == nil) != (scenario == "ok") {
				t.Fatalf("%s: %v", scenario, err)
			}
		})
	}
}
func TestClientRequestUsesCancellation(t *testing.T) {
	c := &Client{root: "/team", http: &http.Client{Transport: transportFunc(func(r *http.Request) (*http.Response, error) {
		<-r.Context().Done()
		return nil, r.Context().Err()
	})}}
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	if _, err := c.Page(ctx, Filter{}, 0); err == nil {
		t.Fatal(fmt.Errorf("cancelled request succeeded"))
	}
}

package queueui

import (
	"context"
	"io"
	"net/http"
	"strings"
	"testing"
)

func TestGuardClientValidationAndSaveBody(t *testing.T) {
	for _, scenario := range []string{"ok", "legacy", "wrong-root", "null-policy", "missing-policy", "duplicate-policy", "too-large", "bad-json"} {
		t.Run(scenario, func(t *testing.T) {
			c := &Client{root: "/team", http: &http.Client{Transport: transportFunc(func(r *http.Request) (*http.Response, error) {
				if r.Method != "PUT" || r.URL.Path != "/v1/claude-guard" {
					t.Fatal(r.Method, r.URL)
				}
				b, err := io.ReadAll(r.Body)
				if err != nil || string(b) != `{"version":1,"enabled":false}` {
					t.Fatal(string(b), err)
				}
				body := `{"project_root":"/team","policy":{"version":1,"enabled":false},"observations":[]}`
				code := 200
				switch scenario {
				case "legacy":
					code = 404
				case "wrong-root":
					body = strings.Replace(body, "/team", "/other", 1)
				case "null-policy":
					body = strings.Replace(body, `{"version":1,"enabled":false}`, `null`, 1)
				case "missing-policy":
					body = `{"project_root":"/team","observations":[]}`
				case "duplicate-policy":
					body = strings.Replace(body, `"enabled":false`, `"enabled":false,"enabled":true`, 1)
				case "too-large":
					body = strings.Repeat(" ", 1024*1024+1)
				case "bad-json":
					body = `{`
				}
				return &http.Response{StatusCode: code, Body: io.NopCloser(strings.NewReader(body))}, nil
			})}}
			_, err := c.SaveGuard(context.Background(), false)
			if (err == nil) != (scenario == "ok") {
				t.Fatal(scenario, err)
			}
		})
	}
}

package queueui

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/avery20024/herdr-async-message-relay/internal/deliver"
	"github.com/avery20024/herdr-async-message-relay/internal/i18n"
	"github.com/avery20024/herdr-async-message-relay/internal/sqlc"
	"github.com/charmbracelet/x/ansi"
)

type retryLoader struct {
	fakeLoader
	targets [][2]string
	err     error
}

func (f *retryLoader) Retry(_ context.Context, id, batch string) error {
	f.targets = append(f.targets, [2]string{id, batch})
	return f.err
}
func retryModel(t *testing.T) (*Model, *retryLoader) {
	t.Helper()
	f := &retryLoader{}
	m := NewModel(t.Context(), f, "/team", i18n.English)
	m.width, m.height = 120, 35
	m.retrySupported = true
	m.loaded = true
	m.rows = []sqlc.BrowseMessagesRow{{ID: "msg-selected", Seq: 1, State: "uncertain", Recipient: "gm", Body: "body", BatchID: sql.NullString{String: "batch-selected", Valid: true}, BatchState: sql.NullString{String: "uncertain", Valid: true}, BatchDetail: sql.NullString{String: "interrupted", Valid: true}}}
	return m, f
}
func TestRetryRequiresConfirmationAndBindsTarget(t *testing.T) {
	m, f := retryModel(t)
	for _, oldKey := range []string{"R", "enter"} {
		m.Update(key(oldKey))
		if m.retryOpen || len(f.targets) != 0 {
			t.Fatal("old shortcut or plain Enter opened retry")
		}
	}
	m.Update(key("ctrl+enter"))
	if !m.retryOpen || len(f.targets) != 0 {
		t.Fatal("opening mutated queue")
	}
	m.Update(key("enter"))
	if len(f.targets) != 0 || m.retryBusy {
		t.Fatal("enter must not confirm")
	}
	m.Update(key("esc"))
	if m.retryOpen || len(f.targets) != 0 {
		t.Fatal("cancel mutated")
	}
	m.Update(key("ctrl+enter"))
	// 自動刷新改變選取，不得讓已開啟的確認框轉向另一則訊息。
	m.rows[0].ID = "msg-new"
	m.rows[0].BatchID.String = "batch-new"
	_, cmd := m.Update(key("y"))
	if cmd == nil || !m.retryBusy {
		t.Fatal("no confirmed retry")
	}
	if _, again := m.Update(key("y")); again != nil {
		t.Fatal("duplicate command")
	}
	result := cmd()
	m.Update(result)
	if len(f.targets) != 1 || f.targets[0] != [2]string{"msg-selected", "batch-selected"} || m.retryOpen {
		t.Fatal(f.targets)
	}
	if !strings.Contains(m.notice, "not a delivery receipt") {
		t.Fatal(m.notice)
	}
}
func TestRetryErrorRequiresFreshSnapshot(t *testing.T) {
	m, f := retryModel(t)
	f.err = errors.New("timeout")
	m.Update(key("ctrl+enter"))
	_, cmd := m.Update(key("y"))
	m.Update(cmd())
	if !m.retryUnconfirmed {
		t.Fatal("timeout must not imply no write")
	}
	m.Update(key("ctrl+enter"))
	if m.retryOpen {
		t.Fatal("blind repeat allowed")
	}
	m.Update(pageMsg{token: m.token, err: errors.New("offline")})
	m.Update(key("ctrl+enter"))
	if m.retryOpen {
		t.Fatal("failed refresh cleared uncertainty")
	}
	m.Update(pageMsg{token: m.token, page: deliver.BrowseResponse{Items: m.rows, RetrySupported: true}})
	m.Update(key("ctrl+enter"))
	if !m.retryOpen {
		t.Fatal("verified fresh state may be retried")
	}
}
func TestRetryUnavailableAndNonUncertain(t *testing.T) {
	m, f := retryModel(t)
	m.retrySupported = false
	m.Update(key("ctrl+enter"))
	if m.retryOpen || len(f.targets) > 0 {
		t.Fatal("old worker unsafe")
	}
	m.retrySupported = true
	for _, state := range []string{"queued", "dispatching", "submitted"} {
		m.rows[0].State = state
		m.Update(key("ctrl+enter"))
		if m.retryOpen {
			t.Fatalf("%s can retry", state)
		}
	}
}
func TestQueueBlockMetadataCardsAndRetryLayouts(t *testing.T) {
	for _, lang := range []i18n.Language{i18n.English, i18n.TraditionalChinese} {
		for _, size := range [][2]int{{120, 35}, {60, 20}, {20, 10}} {
			m, _ := retryModel(t)
			m.language = lang
			m.width, m.height = size[0], size[1]
			m.rows[0].State = "queued"
			m.queueBlocks = []deliver.QueueBlock{{Recipient: "gm", BatchID: "old-batch", Reason: "uncertain_requires_resolution", Detail: "lost\x1b]52;c;bad\a"}}
			meta := strings.Join(m.metadataLines(), "\n")
			if !strings.Contains(strings.ReplaceAll(meta, "\n", ""), "old-batch") {
				t.Fatal(meta)
			}
			if !strings.Contains(ansi.Strip(strings.Join(m.tableLines(), "\n")), "!") {
				t.Fatal("list lacks blocked marker")
			}
			m.rows[0].State = "uncertain"
			m.openRetry()
			for _, line := range strings.Split(ansi.Strip(m.View().Content), "\n") {
				if ansi.StringWidth(line) > size[0] {
					t.Fatalf("overflow %v: %q", size, line)
				}
			}
			selected := m.selected
			m.Update(key("down"))
			if m.selected != selected {
				t.Fatal("modal navigated background")
			}
			if m.retryPageHeight()+len(m.retryHints())+2 > m.contentHeight() {
				t.Fatal("modal clips scrollable warning")
			}
			for range len(m.retryDocument()) + 10 {
				m.Update(key("down"))
			}
			end := m.retryOffset
			if end != max(0, len(m.retryDocument())-m.retryPageHeight()) {
				t.Fatal("scroll exceeded document", end)
			}
			m.Update(key("up"))
			if m.retryOffset != max(0, end-1) {
				t.Fatal("cannot scroll back from bottom")
			}
			m.Update(key("esc"))
			m.queueBlocks = nil
			m.rows[0].State = "queued"
			if !strings.Contains(strings.ReplaceAll(strings.Join(m.metadataContent(false), ""), "\n", ""), m.text("Queue block status unavailable; upgrade worker separately.")) {
				t.Fatal("old worker falsely appears unblocked")
			}
		}
	}
}
func TestRetryClientRequestAndFailures(t *testing.T) {
	for _, scenario := range []string{"ok", "conflict", "legacy", "json", "oversize", "timeout"} {
		t.Run(scenario, func(t *testing.T) {
			c := &Client{root: "/team", http: &http.Client{Transport: transportFunc(func(r *http.Request) (*http.Response, error) {
				if r.Method != "POST" || r.URL.Path != "/v1/batches/batch-test/resolve" {
					t.Fatal(r.Method, r.URL)
				}
				var body map[string]any
				if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
					t.Fatal(err)
				}
				if body["project_root"] != "/team" || body["message_id"] != "msg-test" || body["action"] != "retry" || body["confirm"] != true || body["note"] == "" {
					t.Fatal(body)
				}
				code, data := 200, `{"resolved":true}`
				switch scenario {
				case "conflict":
					code = 409
				case "legacy":
					code = 404
				case "json":
					data = "{"
				case "oversize":
					data = strings.Repeat("x", 4097)
				case "timeout":
					return nil, context.DeadlineExceeded
				}
				return &http.Response{StatusCode: code, Body: io.NopCloser(strings.NewReader(data))}, nil
			})}}
			err := c.Retry(t.Context(), "msg-test", "batch-test")
			if (err == nil) != (scenario == "ok") {
				t.Fatal(err)
			}
		})
	}
}

var _ tea.Msg = retryMsg{}

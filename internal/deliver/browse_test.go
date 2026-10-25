package deliver

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
)

func browse(t *testing.T, h http.Handler, query string) BrowseResponse {
	t.Helper()
	out := httptest.NewRecorder()
	h.ServeHTTP(out, httptest.NewRequest("GET", "/v1/messages"+query, nil))
	if out.Code != 200 {
		t.Fatalf("status %d: %s", out.Code, out.Body.String())
	}
	var result BrowseResponse
	if err := json.Unmarshal(out.Body.Bytes(), &result); err != nil {
		t.Fatal(err)
	}
	return result
}

func TestBrowseHistoryPaginationAndConcurrentInsert(t *testing.T) {
	s, c := fixture(t)
	for i := 0; i < 125; i++ {
		enqueue(t, s, "gm", fmt.Sprintf("original\nbody-%d", i))
	}
	h := API{Store: s, Config: c}.Handler()
	first := browse(t, h, "?limit=50")
	if len(first.Items) != 50 || first.NextBefore == 0 || first.ProjectRoot != c.Root {
		t.Fatalf("bad first page: %+v", first)
	}
	if first.Items[0].Body != "original\nbody-124" {
		t.Fatal("body missing or rewritten")
	}
	// 新訊息插入不能讓既有向下游標重複或漏掉舊訊息。
	enqueue(t, s, "gm", "new message")
	seen := map[string]bool{}
	page := first
	lastSeq := int64(1 << 62)
	for {
		for _, row := range page.Items {
			if seen[row.ID] || row.Seq >= lastSeq {
				t.Fatal("duplicate or unstable ordering")
			}
			seen[row.ID] = true
			lastSeq = row.Seq
			if row.BatchUpdatedAt.Valid {
				t.Fatal("queued message invented delivery time")
			}
		}
		if page.NextBefore == 0 {
			break
		}
		page = browse(t, h, fmt.Sprintf("?limit=50&before=%d", page.NextBefore))
	}
	if len(seen) != 125 {
		t.Fatalf("read %d history rows", len(seen))
	}
	refreshed := browse(t, h, "?limit=1")
	if refreshed.Items[0].Body != "new message" {
		t.Fatal("refresh missed latest")
	}
}

func TestBrowseFiltersCandidatesAndReadOnly(t *testing.T) {
	s, c := fixture(t)
	for _, pair := range [][2]string{{"dev", "gm"}, {"arch", "gm"}, {"dev", "ba"}} {
		if _, err := s.Enqueue(t.Context(), SendRequest{Sender: pair[0], Recipient: pair[1], Body: "unchanged", Key: NewID("test-")}); err != nil {
			t.Fatal(err)
		}
	}
	h := API{Store: s, Config: c}.Handler()
	result := browse(t, h, "?sender=dev&recipient=gm&state=queued&limit=1")
	if len(result.Items) != 1 || result.NextBefore != 0 || len(result.Senders) != 2 || len(result.Recipients) != 2 {
		t.Fatalf("filters/choices: %+v", result)
	}
	if result.Items[0].Sender != "dev" || result.Items[0].Recipient != "gm" {
		t.Fatal("AND filters ignored")
	}
	if len(browse(t, h, "?state=submitted").Items) != 0 {
		t.Fatal("state filter ignored")
	}
	if len(browse(t, h, "?sender=nonexistent").Items) != 0 {
		t.Fatal("unknown role must yield empty history")
	}
	counts, err := s.Q.CountStates(t.Context())
	if err != nil || len(counts) != 1 || counts[0].Count != 3 {
		t.Fatalf("read changed states: %v %v", counts, err)
	}
}

func TestBrowseRejectsInvalidParameters(t *testing.T) {
	s, c := fixture(t)
	h := API{Store: s, Config: c}.Handler()
	for _, query := range []string{"?limit=0", "?limit=101", "?limit=no", "?limit=", "?before=0", "?before=-1", "?before=x", "?before=9223372036854775808", "?state=ready", "?limit=1&limit=2", "?unknown=x", "?sender=x;y=z"} {
		out := httptest.NewRecorder()
		h.ServeHTTP(out, httptest.NewRequest("GET", "/v1/messages"+query, nil))
		if out.Code != 400 {
			t.Fatalf("%s: status %d", query, out.Code)
		}
	}
	empty := browse(t, h, "")
	if empty.Items == nil || len(empty.Items) != 0 || empty.NextBefore != 0 {
		t.Fatalf("empty: %+v", empty)
	}
}

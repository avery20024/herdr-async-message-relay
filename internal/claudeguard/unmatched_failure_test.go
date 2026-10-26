package claudeguard

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestUnmatchedFailurePersistsUnknownAndPreventsDelivery(t *testing.T) {
	for _, enabled := range []bool{true, false} {
		for _, otherPending := range []bool{false, true} {
			t.Run(fmt.Sprintf("enabled=%t/otherPending=%t", enabled, otherPending), func(t *testing.T) {
				m := setup(t)
				apply(t, m, "SessionStart", "")
				if otherPending {
					apply(t, m, "PreToolUse", "other")
				}
				if err := m.ConfigureTeam(t.Context(), enabled); err != nil {
					t.Fatal(err)
				}
				// 失敗本身就是漏事件證據；不可在保存 unknown 前以「未配對」退出。
				apply(t, m, "PostToolUseFailure", "missed-start")
				_, s, err := m.Inspect(t.Context(), "gm")
				if err != nil || s.Phase != "unknown" || s.Pending["other"] != otherPending || s.Pending["missed-start"] {
					t.Fatalf("state=%+v err=%v", s, err)
				}
				if len(s.Recent) != 1 {
					t.Fatal("failure summary missing", s.Recent)
				}
				q := s.Recent[0]
				if q.ToolID != "missed-start" || q.Outcome != "failed" || q.StartedAt != 0 || q.EndedAt <= 0 || q.DurationMS != 0 {
					t.Fatal("invented timing or missing failure", q)
				}
				data, err := os.ReadFile(filepath.Join(m.Dir, "gm", "events.jsonl"))
				if err != nil {
					t.Fatal(err)
				}
				lines := strings.Split(strings.TrimSpace(string(data)), "\n")
				var recorded HistoryEvent
				if err := json.Unmarshal([]byte(lines[len(lines)-1]), &recorded); err != nil {
					t.Fatal(err)
				}
				if recorded.Name != "PostToolUseFailure" || recorded.Phase != "unknown" || recorded.Question == nil || recorded.Question.ToolID != "missed-start" || recorded.Question.StartedAt != 0 {
					t.Fatal("failure event missing", recorded)
				}
				if !enabled {
					release, guarded, err := m.Acquire(t.Context(), "gm", "wC:p5", "session-1")
					if err != nil || guarded {
						t.Fatal("explicit off should retain Herdr-only gate", guarded, err)
					}
					release()
					if err := m.ConfigureTeam(t.Context(), true); err != nil {
						t.Fatal(err)
					}
				}
				allowed(t, m, false)
				apply(t, m, "SessionStart", "")
				allowed(t, m, false)
				if otherPending {
					apply(t, m, "PostToolUse", "other")
					allowed(t, m, false)
				}
			})
		}
	}
}

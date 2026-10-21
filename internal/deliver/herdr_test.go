package deliver

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestCLIHerdrWithFakeBinary(t *testing.T) {
	path := filepath.Join(t.TempDir(), "herdr")
	script := `#!/bin/sh
if [ "$1 $2" = "agent list" ]; then
 printf '%s\n' '{"result":{"agents":[{"cwd":"/team/角色/gm","pane_id":"wC:p5","agent_status":"idle","state_change_seq":10}]}}'
 exit 0
fi
[ "$1" = agent ] && [ "$2" = prompt ] && [ "$3" = wC:p5 ] || exit 7
[ "$5" = --wait ] && [ "$14" = --timeout ] && [ "$15" = 7000 ] || exit 8
printf '%s\n' '{"result":{"agent_status":"working"}}'
`
	// POSIX shell 的兩位數參數需使用大括號。
	script = strings.ReplaceAll(strings.ReplaceAll(script, `"$14"`, `"${14}"`), `"$15"`, `"${15}"`)
	if err := os.WriteFile(path, []byte(script), 0700); err != nil {
		t.Fatal(err)
	}
	h := CLIHerdr{Binary: path, Timeout: time.Second}
	agents, err := h.List(context.Background())
	if err != nil || len(agents) != 1 || agents[0].Pane != "wC:p5" {
		t.Fatalf("%+v %v", agents, err)
	}
	if err = h.Prompt(context.Background(), "wC:p5", "body\n'quoted'"); err != nil {
		t.Fatal(err)
	}
}

func TestCLIRejectsMalformedAndErrorResponses(t *testing.T) {
	path := filepath.Join(t.TempDir(), "herdr")
	for _, response := range []string{`help text`, `{}`, `{"error":{"code":"agent_blocked"}}`, `{"result":null}`} {
		if err := os.WriteFile(path, []byte("#!/bin/sh\nprintf '%s\\n' '"+response+"'\n"), 0700); err != nil {
			t.Fatal(err)
		}
		h := CLIHerdr{Binary: path, Timeout: time.Second}
		if _, err := h.List(context.Background()); err == nil {
			t.Fatal(response)
		}
		if err := h.Prompt(context.Background(), "wC:p5", "body"); err == nil {
			t.Fatal(response)
		}
	}
}

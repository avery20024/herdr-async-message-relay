package deliver

import (
	"context"
	"errors"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"time"

	"github.com/avery20024/herdr-async-message-relay/internal/claudeguard"
	"github.com/avery20024/herdr-async-message-relay/internal/roles"
)

type GuardSettings struct {
	ProjectRoot    string                    `json:"project_root"`
	Policy         claudeguard.Policy        `json:"policy"`
	Observations   []claudeguard.Observation `json:"observations"`
	DiscoveryError string                    `json:"discovery_error,omitempty"`
}

func (a API) guardSettings(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := context.WithTimeout(r.Context(), 2*time.Second)
	defer cancel()
	m := claudeguard.Manager{Dir: filepath.Join(a.Config.StateDir, "claude-guard")}
	if r.Method == http.MethodPut {
		var p claudeguard.Policy
		if !decode(w, r, 1024, &p) {
			return
		}
		if err := m.ConfigureTeam(ctx, p.Enabled); err != nil {
			apiError(w, 500, "unable to save team guard policy; inspect before retrying")
			return
		}
	}
	p, err := m.TeamPolicy()
	if err != nil {
		apiError(w, 500, "unable to read team guard policy")
		return
	}
	result := GuardSettings{ProjectRoot: a.Config.Root, Policy: p, Observations: []claudeguard.Observation{}}
	// 即時座位為主，保留離線角色的既有觀測；查詢失敗不得阻止緊急關閉政策。
	names := map[string]bool{}
	herdr := a.Herdr
	if herdr == nil {
		herdr = CLIHerdr{Binary: a.Config.Herdr, Timeout: a.Config.CommandTimeout}
	}
	agents, listErr := herdr.List(ctx)
	if listErr != nil {
		result.DiscoveryError = "seat discovery unavailable; recorded roles only"
	} else {
		for name := range DiscoverRoles(a.Config.Root, agents) {
			names[name] = true
		}
	}
	entries, readErr := os.ReadDir(m.Dir)
	if readErr != nil && !errors.Is(readErr, os.ErrNotExist) {
		result.DiscoveryError = "unable to enumerate recorded guard roles"
	}
	for _, entry := range entries {
		if entry.IsDir() && roles.ValidName(entry.Name()) {
			names[entry.Name()] = true
		}
	}
	ordered := make([]string, 0, len(names))
	for name := range names {
		ordered = append(ordered, name)
	}
	sort.Strings(ordered)
	for _, name := range ordered {
		result.Observations = append(result.Observations, m.Observation(ctx, name))
	}
	respond(w, 200, result)
}

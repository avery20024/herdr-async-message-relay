package deliver

import (
	"context"
	"net/http"
	"os"
	"path/filepath"
	"time"

	"github.com/avery20024/herdr-async-message-relay/internal/claudeguard"
)

type GuardSettings struct {
	ProjectRoot  string                    `json:"project_root"`
	Policy       claudeguard.Policy        `json:"policy"`
	Observations []claudeguard.Observation `json:"observations"`
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
	roles, err := os.ReadDir(filepath.Join(a.Config.Root, "角色"))
	if err != nil {
		apiError(w, 500, "unable to read team roles")
		return
	}
	for _, role := range roles {
		if !role.IsDir() {
			continue
		}
		result.Observations = append(result.Observations, m.Observation(ctx, role.Name()))
	}
	respond(w, 200, result)
}

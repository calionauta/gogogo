package server

import (
	"context"

	"github.com/calionauta/gogogo/config"
	graphfeat "github.com/calionauta/gogogo/features/graph"
	graphhandlers "github.com/calionauta/gogogo/features/graph/handlers"
	"github.com/calionauta/gogogo/internal/auth"
	appdagnats "github.com/calionauta/gogogo/internal/dagnats"
	graphservice "github.com/calionauta/gogogo/internal/graph"
)

func mountGraph(m *Router, a *auth.Auth, cfg *config.Config) {
	h := graphhandlers.NewHandler(a, cfg)
	h.SetWorkflowDefs([]string{appdagnats.OnboardingWorkflowJSON, appdagnats.ReminderWorkflowJSON})
	if cfg.DagNats.Enabled && cfg.DagNats.HTTPAddr != "" {
		h.SetRunFetcher(runFetcher{c: clientFor(cfg)})
	}
	m.MountFeature("graph", h.Routes())
	MountSubtree(m, graphfeat.AssetPath, graphfeat.StaticHandler())
}

// runFetcher adapts the DagNats HTTP client to the graph RunFetcher seam,
// converting wire steps into graph statuses. Kept here (not in the graph
// handler) so the trimmed graph unit never imports the engine.
type runFetcher struct{ c *appdagnats.Client }

func clientFor(cfg *config.Config) *appdagnats.Client {
	return appdagnats.NewClient("http://" + cfg.DagNats.HTTPAddr)
}

func (f runFetcher) ListRuns(ctx context.Context, workflow string, limit int) ([]graphservice.Run, error) {
	summaries, err := f.c.ListRuns(ctx, workflow, limit)
	if err != nil {
		return nil, err
	}
	out := make([]graphservice.Run, 0, len(summaries))
	for _, s := range summaries {
		steps := make(map[string]graphservice.StepStatus, len(s.Steps))
		for id, st := range s.Steps {
			steps[id] = graphservice.StepStatus{Status: st.Status, Error: st.Error}
		}
		out = append(out, graphservice.Run{
			ID: s.RunID, Workflow: s.WorkflowID, Status: s.Status,
			CreatedAt: s.CreatedAt, Steps: steps,
		})
	}
	return out, nil
}

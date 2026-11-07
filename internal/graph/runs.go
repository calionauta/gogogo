// Live-run overlay: join static workflow defs with recent runs so each step
// node carries its live status. The domain stays engine-free — the HTTP
// fetch lives in internal/dagnats and the handler converts shapes — so
// trimming the engine never breaks this file.
package graph

import (
	"sort"
	"strings"
)

// Run is one recent execution of a workflow, in graph terms.
type Run struct {
	ID        string
	Workflow  string
	Status    string
	CreatedAt string
	Steps     map[string]StepStatus
}

// shortIDLen bounds run-id labels in the canvas.
const shortIDLen = 12

// StepStatus is one step's live status inside a run.
type StepStatus struct {
	Status string
	Error  string
}

// OverlayRuns returns the workflows document with per-step live status from
// the newest run of each workflow. Runs are matched by workflow name;
// extra runs for an unknown workflow are ignored (the defs are the source
// of truth for what the canvas can show). Newest wins by CreatedAt
// (RFC3339 strings sort chronologically); ties break by RunID so the
// output is deterministic.
func OverlayRuns(defs []WorkflowDef, runs []Run) Document {
	doc := Document{Source: DagNatsSource}
	latest := latestRuns(runs)
	for _, wf := range defs {
		run := latest[wf.Name]
		for _, s := range wf.Steps {
			id := wf.Name + ":" + s.ID
			meta := s.Task
			if meta == "" {
				meta = s.Type
			}
			status := ""
			if run != nil {
				if st, ok := run.Steps[s.ID]; ok && st.Status != "" {
					status = st.Status
					if st.Error != "" {
						meta = first(meta, "") + " — " + st.Error
					}
				}
			}
			doc.Nodes = append(doc.Nodes, Node{ID: id, Label: s.ID, Kind: "dagnats", Status: status, Meta: meta})
		}
		for _, s := range wf.Steps {
			for _, dep := range s.Depends {
				doc.Edges = append(doc.Edges, Edge{From: wf.Name + ":" + s.ID, To: wf.Name + ":" + dep, Kind: DependsKind})
			}
		}
		if run != nil {
			doc.Nodes = append(doc.Nodes, Node{
				ID: "run:" + run.ID, Label: "run " + shortID(run.ID), Kind: "run",
				Status: run.Status, Meta: run.Workflow + " · " + run.Status,
			})
			// Run node draws beside its first step; keep the edge list honest
			// without inventing a containment edge the def does not declare.
		}
	}
	layout(&doc)
	return doc
}

// latestRuns picks the newest run per workflow name.
func latestRuns(runs []Run) map[string]*Run {
	out := map[string]*Run{}
	for i := range runs {
		r := &runs[i]
		prev := out[r.Workflow]
		if prev == nil || r.CreatedAt > prev.CreatedAt || (r.CreatedAt == prev.CreatedAt && r.ID > prev.ID) {
			out[r.Workflow] = r
		}
	}
	return out
}

func shortID(id string) string {
	if len(id) <= shortIDLen {
		return id
	}
	return strings.TrimSpace(id[:12])
}

func first(s, fallback string) string {
	if s != "" {
		return s
	}
	return fallback
}

// SortRuns orders runs newest-first for stable handler output.
func SortRuns(runs []Run) {
	sort.Slice(runs, func(i, j int) bool {
		if runs[i].CreatedAt != runs[j].CreatedAt {
			return runs[i].CreatedAt > runs[j].CreatedAt
		}
		return runs[i].ID > runs[j].ID
	})
}

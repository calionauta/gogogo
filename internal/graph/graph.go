// Graph panorama: reads the installed system (capabilities + recipes) and
// answers with a node/edge document the canvas can draw. The domain never
// imports a feature package — it only names them — so trimming the graph
// unit cannot break another unit, and vice versa.
package graph

import (
	"encoding/json"
	"math"
	"os"
	"path/filepath"
	"sort"

	"gopkg.in/yaml.v3"

	"github.com/calionauta/gogogo/internal/capabilities"
	"github.com/calionauta/gogogo/internal/recipe"
)

// Node is one draw-able item. Kind carries the capability kind or "recipe";
// Status is for live sources (dagnats/jobs) and empty for panorama.
type Node struct {
	ID     string `json:"id"`
	Label  string `json:"label"`
	Kind   string `json:"kind"`
	Status string `json:"status,omitempty"`
	Meta   string `json:"meta,omitempty"`
	// X/Y are pre-computed positions (server layout). Canvas2D/WebGL only
	// does pan/zoom/pick. See docs/research for the measured budget.
	X float64 `json:"x,omitempty"`
	Y float64 `json:"y,omitempty"`
}

// Edge connects two nodes. Kind is "depends" for capability dependencies,
// "recipe" for recipe → capability declarations.
type Edge struct {
	From string `json:"from"`
	To   string `json:"to"`
	Kind string `json:"kind"`
}

// Document is the wire shape: nodes, edges, and where the data came from.
type Document struct {
	Source string `json:"source"`
	Nodes  []Node `json:"nodes"`
	Edges  []Edge `json:"edges"`
}

// RecipeKind is the node kind for installed recipes.
const RecipeKind = "recipe"

// DependsKind is the edge kind for declared dependencies.
const DependsKind = "depends"

// DagNatsSource is the document source for workflow graphs.
const DagNatsSource = "dagnats"

// Panorama builds the system map: one node per capability, one per installed
// recipe, edges from DependsOn and from recipe declarations. Layout is a
// deterministic radial scatter the canvas refines with pan/zoom.
func Panorama(recipes []Recipe) Document {
	doc := Document{Source: "panorama"}
	byID := map[string]bool{}
	// Capability nodes grouped by kind for a stable radial order.
	for _, c := range capabilities.All {
		node := Node{ID: c.ID, Label: c.ID, Kind: string(c.Kind), Meta: c.Summary}
		doc.Nodes = append(doc.Nodes, node)
		byID[c.ID] = true
	}
	// Deterministic order: sort nodes by (kind, id).
	sort.Slice(doc.Nodes, func(i, j int) bool {
		if doc.Nodes[i].Kind != doc.Nodes[j].Kind {
			return doc.Nodes[i].Kind < doc.Nodes[j].Kind
		}
		return doc.Nodes[i].ID < doc.Nodes[j].ID
	})
	// Recipe nodes + edges to declared capabilities.
	for _, r := range recipes {
		rid := "recipe:" + r.ID
		doc.Nodes = append(doc.Nodes, Node{ID: rid, Label: r.ID, Kind: RecipeKind, Meta: r.Upstream.Repo})
		byID[rid] = true
		for _, t := range r.Declaration.Tables {
			if !byID[t] {
				doc.Nodes = append(doc.Nodes, Node{ID: t, Label: t, Kind: "table", Meta: "recipe side table"})
				byID[t] = true
			}
			doc.Edges = append(doc.Edges, Edge{From: rid, To: t, Kind: RecipeKind})
		}
	}
	// Dependency edges from the registry.
	for _, c := range capabilities.All {
		for _, d := range c.DependsOn {
			if byID[d] {
				doc.Edges = append(doc.Edges, Edge{From: c.ID, To: d, Kind: DependsKind})
			}
		}
	}
	layout(&doc)
	return doc
}

// Recipe is the panorama half of a receipt: just id + upstream + tables.
// Full receipt parsing lives in internal/recipe; this keeps the graph unit
// from importing the installer's write path.
type Recipe struct {
	ID          string             `yaml:"id"`
	Upstream    recipe.Upstream    `yaml:"upstream"`
	Declaration recipe.Declaration `yaml:"declaration"`
}

// WorkflowDef is one durable flow as the engine sees it: a named DAG of
// steps. The graph unit parses the same JSON the engine runs, so the canvas
// can never show a step the engine does not have.
type WorkflowDef struct {
	Name  string `json:"name"`
	Steps []Step `json:"steps"`
}

// Step is one node of a workflow. Task is empty for engine steps (sleep,
// approval) where Type names the behaviour instead.
type Step struct {
	ID   string `json:"id"`
	Task string `json:"task,omitempty"`
	Type string `json:"type,omitempty"`
	//nolint:tagliatelle // DagNats JSON uses snake_case (depends_on).
	Depends []string `json:"depends_on,omitempty"`
}

// ParseWorkflow decodes one definition. Malformed defs are an error: the
// engine itself would reject them at register time, so the panorama must
// not silently drop a step.
func ParseWorkflow(data []byte) (WorkflowDef, error) {
	var def WorkflowDef
	if err := json.Unmarshal(data, &def); err != nil {
		return WorkflowDef{}, err
	}
	return def, nil
}

// Workflows builds the live source: one node per step, one edge per
// depends_on, namespaced per workflow so two flows never collide. Layout
// is the same deterministic scatter as Panorama.
func Workflows(defs []WorkflowDef) Document {
	doc := Document{Source: DagNatsSource}
	for _, wf := range defs {
		wid := wf.Name
		seen := map[string]bool{}
		for _, s := range wf.Steps {
			id := wid + ":" + s.ID
			label := s.ID
			meta := s.Task
			if meta == "" {
				meta = s.Type
			}
			doc.Nodes = append(doc.Nodes, Node{ID: id, Label: label, Kind: "dagnats", Meta: meta})
			seen[id] = true
			_ = seen
		}
		for _, s := range wf.Steps {
			for _, dep := range s.Depends {
				from := wid + ":" + s.ID
				to := wid + ":" + dep
				doc.Edges = append(doc.Edges, Edge{From: from, To: to, Kind: DependsKind})
			}
		}
	}
	layout(&doc)
	return doc
}

// LoadInstalledRecipes reads <root>/.recipes/*.yml. Missing dir = no recipes
// (fresh tree), never an error.
func LoadInstalledRecipes(root string) ([]Recipe, error) {
	dir := filepath.Join(root, recipe.ReceiptDir)
	files, err := filepath.Glob(filepath.Join(dir, "*.yml"))
	if err != nil {
		return nil, err
	}
	var out []Recipe
	for _, f := range files {
		data, rerr := os.ReadFile(f)
		if rerr != nil {
			return nil, rerr
		}
		var r Recipe
		if yerr := yaml.Unmarshal(data, &r); yerr != nil {
			return nil, yerr
		}
		out = append(out, r)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].ID < out[j].ID })
	return out, nil
}

// layout places nodes on three concentric rings by kind (core inner, plugin
// middle, feature outer; recipes mid). Deterministic: same input, same
// positions — so the canvas never needs a physics step.
func layout(doc *Document) {
	rings := map[string]float64{"core": 1.0, "plugin": 2.0, "feature": 3.0, RecipeKind: 1.6, "table": 2.4}
	counters := map[string]int{}
	for i := range doc.Nodes {
		kind := doc.Nodes[i].Kind
		r, ok := rings[kind]
		if !ok {
			r = 2.5
		}
		j := counters[kind]
		counters[kind]++
		ang := float64(j) * 0.7
		doc.Nodes[i].X = r * 120 * math.Cos(ang)
		doc.Nodes[i].Y = r * 120 * math.Sin(ang)
	}
}

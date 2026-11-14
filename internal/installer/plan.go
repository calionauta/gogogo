// Installer engine: plan preview, receipts, and JSON envelopes
package installer

import (
	"encoding/json"
	"fmt"
	"io"
	"strings"

	"github.com/calionauta/gogogo/catalog"
	"github.com/calionauta/gogogo/internal/capabilities"
	shapekit "github.com/calionauta/gogogo/internal/shape"
)

// keptUnit is what ships: id + kind + one-line summary, so deciders
// reading --format json see what each capability is, not just its id.
type keptUnit struct {
	ID      string `json:"id"`
	Kind    string `json:"kind"`
	Summary string `json:"summary"`
}

// scaffoldPlan is the full preview of what the installer will do. Scaffold
// ships the whole tree: there is no keep/drop decision, and Keep lists every
// capability the scaffold ships, each with its removal class so an agent
// reading --format json has the same three words the registry uses.
type scaffoldPlan struct {
	Name   string     `json:"name"`
	Owner  string     `json:"owner"`
	Module string     `json:"module"`
	Dir    string     `json:"dir"`
	Keep   []keptUnit `json:"keepUnits"`
	// Shape is the product shape the scaffold will apply, and ShapeRemove the
	// entrypoints it prunes. `full` means "every target" — a plain clone.
	Shape       string   `json:"shape"`
	ShapeRemove []string `json:"shapeRemoves,omitempty"`
	DryRun      bool     `json:"dryRun"`
	// Recommend lists the recipes the registry advises at scaffold time.
	// Install lists the ones the operator accepted (via --install-recommended
	// or --install); empty means recommend-only.
	Recommend []string `json:"recommend,omitempty"`
	Install   []string `json:"install,omitempty"`
}

func buildPlan(name, owner, dir, shapeName string, dryRun bool) scaffoldPlan {
	// No keep/drop decision exists: scaffold ships the whole tree, so the plan
	// lists what it keeps (with the kind) and never carries a "drop" list.
	p := scaffoldPlan{
		Name:   name,
		Owner:  owner,
		Module: "github.com/" + owner + "/" + name,
		Dir:    dir,
		Keep:   []keptUnit{},
		DryRun: dryRun,
		Shape:  shapeName,
	}
	if shapeName == "" {
		p.Shape = string(shapekit.Full)
	}
	if cfg, err := shapekit.Parse(shapekit.Config{Shape: shapekit.Shape(p.Shape)}); err == nil {
		keep := map[string]bool{}
		for _, ep := range cfg.Entrypoints {
			keep[ep] = true
		}
		for _, ep := range shapeEntrypoints {
			if !keep[ep] {
				p.ShapeRemove = append(p.ShapeRemove, ep)
			}
		}
	}
	for _, capability := range capabilities.All {
		p.Keep = append(p.Keep, keptUnit{
			ID:      capability.ID,
			Kind:    string(capability.Kind),
			Summary: capability.Summary,
		})
	}
	// The advice comes from ONE file (catalog/recommended.yml), not from a
	// bool on each row: the scaffold and the catalog used to carry separate
	// copies that could disagree.
	p.Recommend = catalog.MustIDs()
	return p
}

func printPlanText(w io.Writer, p scaffoldPlan) {
	fmt.Fprintf(w, "gogogo: scaffolding %s\n", p.Module)
	fmt.Fprintf(w, "  dir:            %s\n", p.Dir)
	fmt.Fprintf(w, "  capabilities:   %s\n", kindLists(p.Keep))
	if p.Shape != "" && p.Shape != string(shapekit.Full) {
		fmt.Fprintf(w, "  shape:          %s (drops %s)\n", p.Shape, strings.Join(p.ShapeRemove, ", "))
	}
	fmt.Fprintln(w, "  removal:        none (full template — remove later with `recipe uninstall`)")
	if len(p.Recommend) > 0 {
		fmt.Fprintf(w, "  recommend:      %s (accept with --install-recommended)\n", strings.Join(p.Recommend, ", "))
	}
	if len(p.Install) > 0 {
		fmt.Fprintf(w, "  install:        %s\n", strings.Join(p.Install, ", "))
	}
}

// kindLists renders the kept units grouped by removal class, in the class
// order core → servant → feature. It reads the plan's own Kind values, so the
// text cannot disagree with the JSON.
func kindLists(units []keptUnit) string {
	order := []capabilities.Kind{capabilities.KindCore, capabilities.KindServant, capabilities.KindFeature}
	var parts []string
	for _, kind := range order {
		var ids []string
		for _, u := range units {
			if u.Kind == string(kind) {
				ids = append(ids, u.ID)
			}
		}
		if len(ids) > 0 {
			parts = append(parts, string(kind)+": "+strings.Join(ids, ", "))
		}
	}
	return strings.Join(parts, "; ")
}

func printPlanJSON(w io.Writer, p scaffoldPlan) error {
	enc := json.NewEncoder(w)
	enc.SetIndent("", "  ")
	return enc.Encode(p)
}

// envelope is the single machine-readable document for apply runs: the plan as
// previewed plus the proof. There is no removal receipt because a scaffold
// removes nothing — the per-recipe receipt lives in `.recipes/<id>.yml`, which
// `recipe status` reads.
type envelope struct {
	Plan     scaffoldPlan `json:"plan"`
	BuildOk  bool         `json:"buildOk"`
	BuildErr string       `json:"buildError,omitempty"`
	Next     nextSteps    `json:"next"`
}

func printEnvelopeJSON(w io.Writer, e envelope) error {
	enc := json.NewEncoder(w)
	enc.SetIndent("", "  ")
	return enc.Encode(e)
}

// applyInstallSelection resolves the recommendation: Install needs
// explicit acceptance (--install-recommended or --install ids).
func applyInstallSelection(p *scaffoldPlan, opt options) {
	if opt.installRecommended {
		p.Install = append(p.Install, p.Recommend...)
	}
	for id := range strings.SplitSeq(opt.installIDs, ",") {
		if id = strings.TrimSpace(id); id != "" {
			p.Install = append(p.Install, id)
		}
	}
	if opt.skipRecommended {
		p.Recommend = nil
	}
}

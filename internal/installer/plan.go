// Installer engine: plan preview, receipts, and JSON envelopes
package installer

import (
	"encoding/json"
	"fmt"
	"io"
	"strings"

	"github.com/calionauta/gogogo/internal/capabilities"
)

// planUnit is the agent- and human-readable rendering of one dropped unit.
// Scaffold keeps every capability, so the scaffold plan carries no dropped
// units; the type is retained only to keep the documented JSON shape stable
// for existing consumers.
type planUnit struct {
	ID         string   `json:"id"`
	Kind       string   `json:"kind"`
	Dirs       []string `json:"dirs"`
	Files      []string `json:"files"`
	Warns      []string `json:"warnings"`
	Note       string   `json:"note,omitempty"`
	Strips     int      `json:"wiringStrips"`
	RuntimeOff string   `json:"runtimeOff,omitempty"`
}

// keptUnit is what ships: id + kind + one-line summary, so deciders
// reading --format json see what each capability is, not just its id.
// Additive (the id lists stay): stable field names are untouched.
type keptUnit struct {
	ID      string `json:"id"`
	Kind    string `json:"kind"`
	Summary string `json:"summary"`
}

// scaffoldPlan is the full preview of what the installer will do. Scaffold
// ships the whole tree: there is no keep/drop decision, and the arrays list
// every capability that the scaffold keeps. JSON field names are stable:
// agents may parse --format json output.
type scaffoldPlan struct {
	Name         string     `json:"name"`
	Owner        string     `json:"owner"`
	Module       string     `json:"module"`
	Dir          string     `json:"dir"`
	KeepPlugins  []string   `json:"keepPlugins"`
	KeepFeatures []string   `json:"keepFeatures"`
	Keep         []keptUnit `json:"keepUnits"`
	Drop         []planUnit `json:"drop"`
	DryRun       bool       `json:"dryRun"`
	// Recommend lists the recipes the registry advises at scaffold time.
	// Install lists the ones the operator accepted (via --install-recommended
	// or --install); empty means recommend-only.
	Recommend []string `json:"recommend,omitempty"`
	Install   []string `json:"install,omitempty"`
}

func buildPlan(name, owner, dir string, dryRun bool) scaffoldPlan {
	// No keep/drop decision exists: scaffold now ships the whole tree.
	// The JSON fields stay populated — keepPlugins/keepFeatures list every
	// plugin/feature the tree keeps — so an agent parsing --format
	// json sees the same shape it always did.
	p := scaffoldPlan{
		Name:         name,
		Owner:        owner,
		Module:       "github.com/" + owner + "/" + name,
		Dir:          dir,
		KeepPlugins:  []string{},
		KeepFeatures: []string{},
		Keep:         []keptUnit{},
		Drop:         []planUnit{},
		DryRun:       dryRun,
	}
	for _, capability := range capabilities.All {
		c := capability.ID
		summary := capability.Summary
		kept := keptUnit{ID: c, Kind: string(capability.Kind), Summary: summary}
		switch capability.Kind {
		case capabilities.KindPlugin:
			p.KeepPlugins = append(p.KeepPlugins, c)
		case capabilities.KindFeature:
			p.KeepFeatures = append(p.KeepFeatures, c)
		}
		p.Keep = append(p.Keep, kept)
		if capability.Recommended {
			p.Recommend = append(p.Recommend, c)
		}
	}
	return p
}

func printPlanText(w io.Writer, p scaffoldPlan) {
	fmt.Fprintf(w, "gogogo: scaffolding %s\n", p.Module)
	fmt.Fprintf(w, "  dir:            %s\n", p.Dir)

	fmt.Fprintf(w, "  capabilities:   %s\n", strings.Join(p.KeepPlugins, ", ")+"; "+strings.Join(p.KeepFeatures, ", "))
	fmt.Fprintln(w, "  removal:        none (full template)")
	if len(p.Recommend) > 0 {
		fmt.Fprintf(w, "  recommend:      %s (accept with --install-recommended)\n", strings.Join(p.Recommend, ", "))
	}
	if len(p.Install) > 0 {
		fmt.Fprintf(w, "  install:        %s\n", strings.Join(p.Install, ", "))
	}
}

func printPlanJSON(w io.Writer, p scaffoldPlan) error {
	enc := json.NewEncoder(w)
	enc.SetIndent("", "  ")
	return enc.Encode(p)
}

// envelope is the single machine-readable document for apply runs:
// the plan as previewed plus what actually changed plus the proof.
type envelope struct {
	Plan     scaffoldPlan `json:"plan"`
	Receipt  Receipt      `json:"receipt"`
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

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
type planUnit struct {
	ID         string   `json:"id"`
	Kind       string   `json:"kind"`
	Scaffold   string   `json:"scaffold"`
	Dirs       []string `json:"dirs"`
	Files      []string `json:"files"`
	Warns      []string `json:"warnings"`
	Note       string   `json:"note,omitempty"`
	Strips     int      `json:"wiringStrips"`
	RuntimeOff string   `json:"runtimeOff,omitempty"`
}

// keptUnit is what stays: id + kind + one-line summary, so deciders
// reading --format json see what each kept unit IS, not just its id.
// Additive (the id lists stay): stable field names are untouched.
type keptUnit struct {
	ID       string `json:"id"`
	Kind     string `json:"kind"`
	Scaffold string `json:"scaffold"`
	Summary  string `json:"summary"`
}

// scaffoldPlan is the full preview of what the installer will do.
// JSON field names are stable: agents may parse --format json output.
type scaffoldPlan struct {
	Name         string     `json:"name"`
	Owner        string     `json:"owner"`
	Module       string     `json:"module"`
	Dir          string     `json:"dir"`
	KeepPlugins  []string   `json:"keepPlugins"`
	KeepFeatures []string   `json:"keepFeatures"`
	KeepOptIn    []string   `json:"keepOptIn"`
	Keep         []keptUnit `json:"keepUnits"`
	Drop         []planUnit `json:"drop"`
	DryRun       bool       `json:"dryRun"`
	// Preset names the --preset that produced this plan ("" for explicit
	// keep lists). Additive: parsers that ignore unknown fields are unaffected.
	Preset string `json:"preset,omitempty"`
}

func buildPlan(name, owner, dir string, dryRun bool) scaffoldPlan {
	// No keep/drop decision exists: scaffold now ships the whole tree.
	// The JSON fields stay populated — keepPlugins/keepFeatures list every
	// offered plugin/feature the tree keeps — so an agent parsing --format
	// json sees the same shape it always did.
	p := scaffoldPlan{
		Name:         name,
		Owner:        owner,
		Module:       "github.com/" + owner + "/" + name,
		Dir:          dir,
		KeepPlugins:  []string{},
		KeepFeatures: []string{},
		KeepOptIn:    []string{},
		Keep:         []keptUnit{},
		Drop:         []planUnit{},
		DryRun:       dryRun,
	}
	for _, capability := range capabilities.All {
		c := capability.ID
		summary := capability.Summary
		kept := keptUnit{ID: c, Kind: string(capability.Kind), Scaffold: "", Summary: summary}
		switch capability.Kind {
		case capabilities.KindPlugin:
			p.KeepPlugins = append(p.KeepPlugins, c)
		case capabilities.KindFeature:
			p.KeepFeatures = append(p.KeepFeatures, c)
		}
		p.Keep = append(p.Keep, kept)
	}
	return p
}

func printPlanText(w io.Writer, p scaffoldPlan) {
	fmt.Fprintf(w, "gogogo: scaffolding %s\n", p.Module)
	fmt.Fprintf(w, "  dir:            %s\n", p.Dir)

	fmt.Fprintf(w, "  keep plugins:   %s\n", strings.Join(p.KeepPlugins, ", "))
	fmt.Fprintf(w, "  keep features:  %s\n", strings.Join(p.KeepFeatures, ", "))
	if len(p.KeepOptIn) > 0 {
		fmt.Fprintf(w, "  keep opt-in:    %s\n", strings.Join(p.KeepOptIn, ", "))
	}
	if len(p.Drop) == 0 {
		fmt.Fprintln(w, "  trim:           nothing (full template)")
		return
	}
	fmt.Fprintln(w, "  trim:           nothing (full template)")
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

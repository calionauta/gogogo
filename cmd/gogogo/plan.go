package main

import (
	"encoding/json"
	"fmt"
	"io"
	"strings"
)

// planUnit is the agent- and human-readable rendering of one dropped unit.
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

// scaffoldPlan is the full preview of what the installer will do.
// JSON field names are stable: agents may parse --format json output.
type scaffoldPlan struct {
	Name         string     `json:"name"`
	Owner        string     `json:"owner"`
	Module       string     `json:"module"`
	Dir          string     `json:"dir"`
	KeepPlugins  []string   `json:"keepPlugins"`
	KeepFeatures []string   `json:"keepFeatures"`
	Drop         []planUnit `json:"drop"`
	DryRun       bool       `json:"dryRun"`
}

func buildPlan(name, owner, dir string, keep keepSet, drop []trimUnit, dryRun bool) scaffoldPlan {
	p := scaffoldPlan{
		Name:         name,
		Owner:        owner,
		Module:       "github.com/" + owner + "/" + name,
		Dir:          dir,
		KeepPlugins:  sortedKeys(keep.plugins),
		KeepFeatures: sortedKeys(keep.features),
		Drop:         []planUnit{},
		DryRun:       dryRun,
	}
	for _, u := range drop {
		m := u.meta()
		p.Drop = append(p.Drop, planUnit{
			ID:         u.id,
			Kind:       string(m.kind),
			Dirs:       m.dirs,
			Files:      m.files,
			Warns:      m.warns,
			Note:       m.note,
			Strips:     len(u.mainStrips) + len(u.desktopStrips) + len(u.extraStrips),
			RuntimeOff: m.runtimeOff,
		})
	}
	return p
}

// needsTemplGen reports whether any dropped unit edited a .templ file that
// stays on disk (navbar links, sound call sites, brand retarget). Whole-dir
// .templ deletions need no regen; in-place edits do.
func needsTemplGen(drop []trimUnit) bool {
	for _, u := range drop {
		for _, es := range u.extraStrips {
			if strings.HasSuffix(es.path, ".templ") {
				return true
			}
		}
		for _, ed := range u.extraDrops {
			if strings.HasSuffix(ed.path, ".templ") {
				return true
			}
		}
		for _, rp := range u.replaces {
			if strings.HasSuffix(rp.path, ".templ") {
				return true
			}
		}
	}
	return false
}

func printPlanText(w io.Writer, p scaffoldPlan) {
	fmt.Fprintf(w, "gogogo: scaffolding %s\n", p.Module)
	fmt.Fprintf(w, "  dir:            %s\n", p.Dir)
	fmt.Fprintf(w, "  keep plugins:   %s\n", strings.Join(p.KeepPlugins, ", "))
	fmt.Fprintf(w, "  keep features:  %s\n", strings.Join(p.KeepFeatures, ", "))
	if len(p.Drop) == 0 {
		fmt.Fprintln(w, "  trim:           nothing (full template)")
		return
	}
	fmt.Fprintln(w, "  trim:")
	for _, u := range p.Drop {
		nFiles := len(u.Dirs) + len(u.Files)
		fmt.Fprintf(w, "    - %-12s (%s, %d path(s), %d wiring strip(s))",
			u.ID, u.Kind, nFiles, u.Strips)
		if u.RuntimeOff != "" {
			fmt.Fprintf(w, " [reversible: %s]", u.RuntimeOff)
		}
		fmt.Fprintln(w)
	}
	fmt.Fprintln(w, "  consequences (read before confirming):")
	n := 0
	for _, u := range p.Drop {
		for _, warn := range u.Warns {
			n++
			fmt.Fprintf(w, "    %d. [%s] %s\n", n, u.ID, warn)
		}
		if u.Note != "" {
			n++
			fmt.Fprintf(w, "    %d. [%s] note: %s\n", n, u.ID, u.Note)
		}
	}
	if n == 0 {
		fmt.Fprintln(w, "    none — pure deletions with their wiring calls.")
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
}

func printEnvelopeJSON(w io.Writer, e envelope) error {
	enc := json.NewEncoder(w)
	enc.SetIndent("", "  ")
	return enc.Encode(e)
}

// printReceiptText reports per-unit counts plus every missed marker.
// Missed markers are manifest drift: loud, but not fatal while the
// proof build passes (re-runs must stay idempotent).
func printReceiptText(w io.Writer, rc *Receipt) {
	applied, missed := 0, 0
	for _, u := range rc.Units {
		applied += u.StripsApplied
		missed += len(u.StripsMissed)
	}
	fmt.Fprintf(w, "gogogo: receipt: %d strip(s) applied, %d missed\n", applied, missed)
	for _, u := range rc.Units {
		for _, m := range u.StripsMissed {
			fmt.Fprintf(w, "gogogo: WARN: missed strip [%s] %s\n", u.ID, m)
		}
	}
}

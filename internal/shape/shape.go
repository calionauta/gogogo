// Package shape owns the one answer to "which product is this tree?"
//
// A shape is a closed enum, not a per-unit checkbox. It selects which
// entrypoints the tree ships and, through that, which capabilities are
// plausible; the record lives in `.gogogo/shape.yml`, written once at scaffold
// time and read by the gate. A tree with no file is `full`, which is what every
// checkout before this package was.
//
// Why one list: the failure mode that retired the keep/drop prompt was three
// inventories — the CLI plan, the manifest and the docs — drifting apart. A
// shape is a single value with a single legal set of entrypoints per value, so
// "what does `api` mean" has exactly one answer in the tree.
package shape

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"gopkg.in/yaml.v3"
)

// Shape is one product shape. The values are the wire format (the file, the
// plan JSON, the MCP schema), so they never change spelling.
type Shape string

const (
	// Full ships every target; it is the default and today's behaviour.
	Full Shape = "full"
	// Web is the browser app: `cmd/web` only.
	Web Shape = "web"
	// API is `cmd/web` serving /api/* and health with no page routes. It is
	// not a second binary; the page routes are the part it removes.
	API Shape = "api"
	// Desktop is the Wails shell plus the web server it embeds.
	Desktop Shape = "desktop"
	// GUI is the gogpu/ui native shell, which renders the same components.
	GUI Shape = "gui"
	// Mobile is the Wails Android shell plus the web server it embeds.
	Mobile Shape = "mobile"
)

// All is the enum in the order the docs present it. A new shape is a line here
// and a row in docs/shape.md — and a deliberate decision, because every shape
// is a promise that its subset builds and passes the same gates.
var All = []Shape{Full, Web, API, Desktop, GUI, Mobile}

// Path is the machine-read record, relative to the tree's root.
const Path = ".gogogo/shape.yml"

// legalEntrypoints is the closed set a shape may declare. A shape never
// invents a boot path: `api` is `cmd/web`, not `cmd/api`.
var legalEntrypoints = map[Shape][]string{
	Full:    {cmdDesktop, cmdGUI, cmdMobile, cmdWeb},
	Web:     {cmdWeb},
	API:     {cmdWeb},
	Desktop: {cmdDesktop, cmdWeb},
	GUI:     {cmdGUI},
	Mobile:  {cmdMobile, cmdWeb},
}

// The entrypoint paths, spelled once: the enum's table, the primary map and the
// tree checks all address the same strings.
const (
	cmdWeb     = "cmd/web"
	cmdDesktop = "cmd/desktop"
	cmdGUI     = "cmd/gui"
	cmdMobile  = "cmd/mobile"
)

// primaryEntrypoint is the one binary a shape exists for. A declared
// entrypoints list that omits it describes a tree nobody can boot.
var primaryEntrypoint = map[Shape]string{
	Full:    cmdWeb,
	Web:     cmdWeb,
	API:     cmdWeb,
	Desktop: cmdDesktop,
	GUI:     cmdGUI,
	Mobile:  cmdMobile,
}

// Config is the file's contents plus the shape it resolves to.
type Config struct {
	// Shape is the declared shape, or Full when the tree has no record.
	Shape Shape `yaml:"shape"`
	// Entrypoints are the binaries this tree ships. Empty means "the shape's
	// own set", which is what a hand-written file rarely needs to repeat.
	Entrypoints []string `yaml:"entrypoints,omitempty"`
	// Removed are repo-relative dirs and files the shape deliberately dropped.
	// The registry conformance tests use the same list shape (removed-paths:).
	Removed []string `yaml:"removed,omitempty"`

	// Declared reports whether a file existed. A tree without one is Full by
	// default, but the docs say so rather than implying a choice was made.
	Declared bool `yaml:"-"`
}

// Parse validates a shape declaration without touching a tree.
func Parse(c Config) (Config, error) {
	if c.Shape == "" {
		c.Shape = Full
	}
	if !Valid(c.Shape) {
		return Config{}, fmt.Errorf("shape: %q is not one of %s", c.Shape, legalShapes(All))
	}
	if len(c.Entrypoints) == 0 {
		c.Entrypoints = append([]string{}, legalEntrypoints[c.Shape]...)
	}
	seen := map[string]bool{}
	for _, ep := range c.Entrypoints {
		switch {
		case !legalFor(c.Shape)[ep]:
			return Config{}, fmt.Errorf("shape: %s cannot ship %s (it ships %s)",
				c.Shape, ep, legalPaths(legalEntrypoints[c.Shape]))
		case seen[ep]:
			return Config{}, fmt.Errorf("shape: %s is declared twice", ep)
		}
		seen[ep] = true
	}
	if want := primaryEntrypoint[c.Shape]; !seen[want] {
		return Config{}, fmt.Errorf("shape: %s must declare its own entrypoint %s", c.Shape, want)
	}
	if strings.Contains(strings.Join(c.Removed, " "), "..") {
		return Config{}, errors.New("shape: removed paths must be repo-relative")
	}
	return c, nil
}

// Valid reports whether s is a known shape.
func Valid(s Shape) bool {
	_, ok := legalEntrypoints[s]
	return ok
}

// Load reads the record at root. A missing file is Full, not an error: every
// tree scaffolded before shapes existed is full by definition.
func Load(root string) (Config, error) {
	raw, err := os.ReadFile(filepath.Join(root, Path))
	if os.IsNotExist(err) {
		return Config{Shape: Full, Entrypoints: legalEntrypoints[Full]}, nil
	}
	if err != nil {
		return Config{}, fmt.Errorf("shape: read %s: %w", Path, err)
	}
	var c Config
	if err := yaml.Unmarshal(raw, &c); err != nil {
		return Config{}, fmt.Errorf("shape: parse %s: %w", Path, err)
	}
	c.Declared = true
	return Parse(c)
}

// Check is the gate: the record agrees with itself and with the tree. It
// returns human-readable problems, empty when the tree is consistent.
//
// Text-only on purpose: the build proof belongs to CI (which builds the
// declared entrypoints), and a gate that compiles cannot run on every commit.
func Check(root string) ([]string, error) {
	c, err := Load(root)
	if err != nil {
		// The load error is the verdict: a file that does not parse is a
		// problem to print, not a second failure channel to handle.
		return []string{err.Error()}, nil //nolint:nilerr // see comment
	}
	var problems []string
	keep := map[string]bool{}
	for _, ep := range c.Entrypoints {
		keep[ep] = true
		if err := checkMainPackage(root, ep, "entrypoint"); err != nil {
			problems = append(problems, err.Error())
		}
	}
	for _, rel := range c.Removed {
		if keep[rel] {
			problems = append(problems,
				fmt.Sprintf("shape: %s is both an entrypoint and a removed path", rel))
			continue
		}
		if _, err := os.Stat(filepath.Join(root, filepath.FromSlash(rel))); err == nil {
			problems = append(problems,
				fmt.Sprintf("shape: %s is declared removed but still exists", rel))
		} else if !os.IsNotExist(err) {
			problems = append(problems, fmt.Sprintf("shape: stat %s: %v", rel, err))
		}
	}
	sort.Strings(problems)
	return problems, nil
}

// checkMainPackage proves a path is a directory holding a `package main`.
func checkMainPackage(root, rel, kind string) error {
	dir := filepath.Join(root, filepath.FromSlash(rel))
	entries, err := os.ReadDir(dir)
	if err != nil {
		return fmt.Errorf("shape: %s %s does not exist: %w", kind, rel, err)
	}
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".go") || strings.HasSuffix(e.Name(), "_test.go") {
			continue
		}
		raw, err := os.ReadFile(filepath.Join(dir, e.Name()))
		if err != nil {
			return fmt.Errorf("shape: read %s: %w", filepath.Join(rel, e.Name()), err)
		}
		if strings.HasPrefix(string(raw), "package main") ||
			strings.Contains(string(raw), "\npackage main") {
			return nil
		}
	}
	return fmt.Errorf("shape: %s %s has no `package main`", kind, rel)
}

// legalFor returns the entrypoint set a shape may declare.
func legalFor(s Shape) map[string]bool {
	out := make(map[string]bool, len(legalEntrypoints[s]))
	for _, ep := range legalEntrypoints[s] {
		out[ep] = true
	}
	return out
}

// legalShapes joins the enum for an error message.
func legalShapes(values []Shape) string {
	names := make([]string, 0, len(values))
	for _, v := range values {
		names = append(names, string(v))
	}
	return strings.Join(names, "|")
}

// legalPaths joins a closed entrypoint set for an error message.
func legalPaths(values []string) string { return strings.Join(values, "|") }

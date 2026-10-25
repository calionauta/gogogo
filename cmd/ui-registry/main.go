// SCOPE:core - DO NOT REMOVE - the UI registry reader (agent-facing).
//
// `go run ./cmd/ui-registry list` shows the approved compositions; `show <id>`
// prints one with its rule of use, the anti-pattern it replaces, the canonical
// source file, and the doc anchor. This is the assembly surface the 2026
// research measured as the biggest compliance lever: an agent that copies an
// approved shape beats an agent re-deriving the UI from prose and a long style
// guide. The registry is validated by `make check-classes`, so a stale entry
// fails the build instead of misleading the next reader.
package main

import (
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/calionauta/gogogo/internal/uiregistry"
)

func main() {
	root := flag.String("root", ".", "repo root")
	flag.Usage = usage
	flag.Parse()

	reg, err := uiregistry.Load(*root)
	if err != nil {
		fmt.Fprintf(os.Stderr, "ui-registry: %v\n", err)
		os.Exit(1)
	}

	args := flag.Args()
	switch {
	case len(args) == 0 || args[0] == "list":
		fmt.Print(list(reg))
	case args[0] == "show" && len(args) == 2:
		out, ok := show(reg, args[1], *root)
		if !ok {
			fmt.Fprintf(os.Stderr, "ui-registry: no entry %q (try `list`)\n", args[1])
			os.Exit(1)
		}
		fmt.Print(out)
	default:
		flag.Usage()
		os.Exit(2)
	}
}

func usage() {
	fmt.Fprintf(os.Stderr, `usage: ui-registry [list | show <id>]

  list        approved compositions (id, title, source)
  show <id>   one composition: when to use it, what to avoid, where the
              canonical markup lives, and the doc anchor

Copy the source shape instead of inventing one. The registry is validated by
make check-classes; docs/ui-compositions.md is its human half.
`)
}

// list renders every entry as one aligned line, sorted by id so two runs never
// differ.
func list(reg *uiregistry.Registry) string {
	entries := append([]uiregistry.Entry(nil), reg.Entries...)
	sort.Slice(entries, func(i, j int) bool { return entries[i].ID < entries[j].ID })
	idWidth, titleWidth := 0, 0
	for _, e := range entries {
		idWidth = max(idWidth, len(e.ID))
		titleWidth = max(titleWidth, len(e.Title))
	}
	var b strings.Builder
	for _, e := range entries {
		fmt.Fprintf(&b, "%-*s  %-*s  %s\n", idWidth, e.ID, titleWidth, e.Title, e.Source)
	}
	fmt.Fprintf(&b, "\n%d compositions — `show <id>` for the rule of use.\n", len(entries))
	return b.String()
}

// show renders one entry, plus where its markup actually lives. The excerpt is
// read from the source at call time — the registry points at the truth instead
// of carrying a copy that would drift.
func show(reg *uiregistry.Registry, id, root string) (string, bool) {
	e, ok := reg.Find(id)
	if !ok {
		return "", false
	}
	var b strings.Builder
	fmt.Fprintf(&b, "%s — %s\n\n", e.ID, e.Title)
	fmt.Fprintf(&b, "When:   %s\n", e.When)
	fmt.Fprintf(&b, "Avoid:  %s\n", e.Avoid)
	fmt.Fprintf(&b, "Source: %s\n", e.Source)
	fmt.Fprintf(&b, "Doc:    %s\n", e.Doc)
	fmt.Fprintf(&b, "Classes this entry promises: %s\n", strings.Join(e.Shows, ", "))
	if lines := sourceExcerpt(root, e); len(lines) > 0 {
		b.WriteString("\nWhere it lives:\n")
		for _, line := range lines {
			b.WriteString("  " + line + "\n")
		}
	}
	b.WriteString("\nRead the source and copy its shape; do not re-derive the markup.\n")
	return b.String(), true
}

// sourceExcerpt returns the source lines that carry the entry's classes, best
// first: a line matching several of them IS the composition (the one holding
// `join` and `join-item`), while a line matching one is usually a neighbour.
// The excerpt is read at call time, so the registry points at the truth instead
// of carrying a copy that would drift.
func sourceExcerpt(root string, e uiregistry.Entry) []string {
	// #nosec G304 G703 -- local dev tool; the path comes from the committed registry.
	raw, err := os.ReadFile(filepath.Join(root, filepath.FromSlash(e.Source)))
	if err != nil {
		return nil
	}
	lines := strings.Split(string(raw), "\n")
	scores := map[int]int{}
	for i, line := range lines {
		for _, class := range e.Shows {
			if strings.Contains(line, class) {
				scores[i]++
			}
		}
	}
	order := make([]int, 0, len(scores))
	for i := range scores {
		order = append(order, i)
	}
	sort.Slice(order, func(a, b int) bool {
		if scores[order[a]] != scores[order[b]] {
			return scores[order[a]] > scores[order[b]]
		}
		return order[a] < order[b]
	})
	var out []string
	for _, i := range order {
		if len(out) == maxExcerptLines {
			break
		}
		out = append(out, fmt.Sprintf("%s:%d  %s", e.Source, i+1, clip(strings.TrimSpace(lines[i]))))
	}
	return out
}

// clip keeps an excerpt readable without hiding which line it is.
func clip(line string) string {
	if len(line) <= maxExcerptWidth {
		return line
	}
	return line[:maxExcerptWidth] + "…"
}

const (
	maxExcerptLines = 4
	maxExcerptWidth = 100
)

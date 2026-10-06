// SCOPE:layer=infra,removal=plugin — installer engine: interactive form, numbered menus, keep-set parsing
package installer

import (
	"bufio"
	"errors"
	"fmt"
	"io"
	"regexp"
	"sort"
	"strconv"
	"strings"

	"github.com/calionauta/gogogo/internal/capabilities"
)

// NAME_RE mirrors scripts/rename-project.py: the name becomes a Go module
// path segment and a container name, so a bad character fails much later in
// a much more confusing place.
var nameRE = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._-]*$`)

func validateName(name string) error {
	if !nameRE.MatchString(name) {
		return fmt.Errorf(
			"invalid name %q — start with a letter or digit; "+
				"letters, digits, dot, hyphen, underscore only",
			name,
		)
	}
	if name == "gogogo" {
		return errors.New("nothing to do — that is the template name itself")
	}
	return nil
}

type formResult struct {
	name     string
	owner    string
	plugins  string
	features string
}

// promptForm is the stdlib interactive form. It asks the same four questions
// a future Charm Huh form would ask, in the same order, so the upgrade only
// swaps the renderer. Closed-choice questions render a numbered menu (stable
// order = manifest order): humans answer with numbers, ids, or both.
func promptForm(stdin io.Reader, stdout io.Writer) formResult {
	in := bufio.NewScanner(stdin)
	ask := func(label, def string) string {
		if def != "" {
			fmt.Fprintf(stdout, "%s [%s]: ", label, def)
		} else {
			fmt.Fprintf(stdout, "%s: ", label)
		}
		if !in.Scan() {
			return def
		}
		if v := strings.TrimSpace(in.Text()); v != "" {
			return v
		}
		return def
	}

	res := formResult{}
	res.name = ask("Project name", "")
	res.owner = ask("GitHub owner", "calionauta")
	res.plugins = askKeep(in, stdout, "plugins", capabilities.KindPlugin)
	res.features = askKeep(in, stdout, "features", capabilities.KindFeature)
	return res
}

// askKeep renders one closed-choice question as a numbered menu and loops
// until the answer parses. Returns a parseKeep-ready string: "" (keep all),
// "none" (drop all), or comma-joined unit ids. Numbers are 1-based positions
// in the menu shown above them — never stored, never passed to flags.
func askKeep(in *bufio.Scanner, stdout io.Writer, dimension string, kind capabilities.Kind) string {
	var options []string
	summaries := map[string]string{}
	byID := capabilities.ByID()
	for _, u := range manifestUnits {
		if u.meta().kind != kind {
			continue
		}
		options = append(options, u.id)
		if len(u.caps()) > 0 {
			if c, ok := byID[u.caps()[0]]; ok {
				summaries[u.id] = c.Summary
			}
		}
	}
	label := "Keep " + dimension
	fmt.Fprintf(stdout, "%s (numbers and/or ids, comma-separated).\n", label)
	for i, id := range options {
		fmt.Fprintf(stdout, "  %d) %-12s %s\n", i+1, id, summaries[id])
	}
	fmt.Fprintf(stdout, "Empty = keep all. \"none\" = drop all.\n")
	for {
		fmt.Fprintf(stdout, "%s [%s]: ", label, strings.Join(options, ","))
		if !in.Scan() {
			return ""
		}
		ids, all, err := parseSelection(strings.TrimSpace(in.Text()), options)
		if err != nil {
			fmt.Fprintf(stdout, "gogogo: %v — try again.\n", err)
			continue
		}
		if all {
			return ""
		}
		if len(ids) == 0 {
			return keepNone
		}
		return strings.Join(ids, ",")
	}
}

// parseSelection maps one answer to unit ids. Empty means keep-all (all
// true); "none" means keep nothing; otherwise every comma-separated token
// must be a 1-based menu number or a unit id. Unknown tokens error naming
// every valid choice, so neither humans nor agents can silently misspell.
func parseSelection(input string, options []string) (ids []string, all bool, err error) {
	if input == "" {
		return nil, true, nil
	}
	if strings.EqualFold(input, keepNone) {
		return []string{}, false, nil
	}
	byID := map[string]bool{}
	for _, id := range options {
		byID[id] = true
	}
	seen := map[string]bool{}
	for tok := range strings.SplitSeq(input, ",") {
		tok = strings.TrimSpace(tok)
		if tok == "" {
			continue
		}
		var id string
		if n, convErr := strconv.Atoi(tok); convErr == nil {
			if n < 1 || n > len(options) {
				return nil, false, selectionError(tok, options)
			}
			id = options[n-1]
		} else if byID[tok] {
			id = tok
		} else {
			return nil, false, selectionError(tok, options)
		}
		if !seen[id] {
			seen[id] = true
			ids = append(ids, id)
		}
	}
	return ids, false, nil
}

func selectionError(tok string, options []string) error {
	numbered := make([]string, 0, len(options))
	for i, id := range options {
		numbered = append(numbered, fmt.Sprintf("%d=%s", i+1, id))
	}
	return fmt.Errorf("unknown %q (valid: %s, empty = all, none = drop all)",
		tok, strings.Join(numbered, ", "))
}

type keepSet struct {
	plugins  map[string]bool
	features map[string]bool
}

func parseKeep(plugins, features string) keepSet {
	k := keepSet{plugins: map[string]bool{}, features: map[string]bool{}}
	all := make([]string, 0, len(manifestUnits))
	for _, u := range manifestUnits {
		all = append(all, u.id)
	}
	// Empty means keep-all in that dimension (the prompt asks plugins
	// and features as separate questions). Never mirror one dimension
	// into the other: an empty features answer keeps every feature.
	// The literal "none" keeps nothing (drop everything in scope).
	if strings.TrimSpace(plugins) == "" {
		plugins = strings.Join(all, ",")
	} else if strings.TrimSpace(plugins) == keepNone {
		plugins = ""
	}
	if strings.TrimSpace(features) == "" {
		features = strings.Join(all, ",")
	} else if strings.TrimSpace(features) == keepNone {
		features = ""
	}
	for p := range strings.SplitSeq(plugins, ",") {
		if v := strings.TrimSpace(p); v != "" {
			k.plugins[v] = true
		}
	}
	for p := range strings.SplitSeq(features, ",") {
		if v := strings.TrimSpace(p); v != "" {
			k.features[v] = true
		}
	}
	return k
}

func sortedKeys(m map[string]bool) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

// unitIDsOfKind lists installer unit ids of one registry kind, in manifest
// order. It feeds the interactive prompt so plugins and features are asked
// as separate questions sharing one vocabulary.
// unitIDsOfKind lists installer unit ids of one registry kind, in manifest
// order. Feed for menus and prompts sharing one vocabulary.
func unitIDsOfKind(kind capabilities.Kind) []string {
	var ids []string
	for _, u := range manifestUnits {
		if u.meta().kind == kind {
			ids = append(ids, u.id)
		}
	}
	return ids
}

// keepNone keeps nothing in a dimension (drop everything in scope).
// Empty means keep-all; unknown ids fail fast in checkKeepIDs.
const keepNone = "none"

// checkKeepIDs fails fast on unknown unit ids: a typo like --plugins
// dagnat must error listing valid ids, never silently keep everything.
func checkKeepIDs(plugins, features string) error {
	known := map[string]bool{}
	for _, u := range manifestUnits {
		known[u.id] = true
	}
	var unknown []string
	seen := map[string]bool{}
	collect := func(raw string) {
		for p := range strings.SplitSeq(raw, ",") {
			id := strings.TrimSpace(p)
			if id == "" || id == keepNone || seen[id] {
				continue
			}
			seen[id] = true
			if !known[id] {
				unknown = append(unknown, id)
			}
		}
	}
	collect(plugins)
	collect(features)
	if len(unknown) > 0 {
		sort.Strings(unknown)
		return fmt.Errorf("unknown unit id(s) %s (valid: %s)",
			strings.Join(unknown, ", "), strings.Join(sortedKeys(known), ", "))
	}
	return nil
}

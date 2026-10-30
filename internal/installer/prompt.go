// SCOPE:plugin — installer engine: interactive form, numbered menus, keep-set parsing
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
	optIn    string
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
	res.plugins = askKeep(in, stdout, "plugins", capabilities.ScaffoldDefault, capabilities.KindPlugin)
	res.features = askKeep(in, stdout, "features", capabilities.ScaffoldDefault, capabilities.KindFeature)
	res.optIn = askOptIn(in, stdout)
	return res
}

// askKeep renders one default-class question. Empty keeps every default unit
// in the dimension; "none" drops them all.
func askKeep(
	in *bufio.Scanner, stdout io.Writer, dimension string,
	scaffold capabilities.Scaffold, kind capabilities.Kind,
) string {
	options := unitIDsOf(scaffold, kind)
	summaries := map[string]string{}
	byID := capabilities.ByID()
	for _, id := range options {
		if c, ok := byID[id]; ok {
			summaries[id] = c.Summary
		}
	}
	label := "Keep " + dimension
	fmt.Fprintf(stdout, "%s (numbers and/or ids, comma-separated).\n", label)
	for i, id := range options {
		fmt.Fprintf(stdout, "  %d) %-12s %s\n", i+1, id, summaries[id])
	}
	fmt.Fprintf(stdout, "Empty or \"all\" = keep all defaults. \"none\" = drop all.\n")
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

// askOptIn renders the opt-in group: empty (or "none") keeps none, "all"
// keeps every opt-in unit, and numbers/ids pick a subset. The question is
// skipped when the registry has no opt-in unit.
func askOptIn(in *bufio.Scanner, stdout io.Writer) string {
	options := unitIDsOf(capabilities.ScaffoldOptIn, "")
	if len(options) == 0 {
		return ""
	}
	summaries := map[string]string{}
	byID := capabilities.ByID()
	for _, id := range options {
		if c, ok := byID[id]; ok {
			summaries[id] = c.Summary
		}
	}
	label := "Add opt-in units"
	fmt.Fprintf(stdout, "%s (numbers and/or ids, comma-separated).\n", label)
	for i, id := range options {
		fmt.Fprintf(stdout, "  %d) %-12s %s\n", i+1, id, summaries[id])
	}
	fmt.Fprintf(stdout, "Empty = none. \"all\" = all opt-in units.\n")
	for {
		fmt.Fprintf(stdout, "%s [%s]: ", label, strings.Join(options, ","))
		if !in.Scan() {
			return ""
		}
		answer := strings.TrimSpace(in.Text())
		if answer == "" || strings.EqualFold(answer, keepNone) {
			return ""
		}
		if strings.EqualFold(answer, "all") {
			return strings.Join(options, ",")
		}
		ids, _, err := parseSelection(answer, options)
		if err != nil {
			fmt.Fprintf(stdout, "gogogo: %v — try again.\n", err)
			continue
		}
		return strings.Join(ids, ",")
	}
}

// parseSelection maps one answer to unit ids. Empty means keep-all (all
// true); "none" means keep nothing; otherwise every comma-separated token
// must be a 1-based menu number or a unit id. Unknown tokens error naming
// every valid choice, so neither humans nor agents can silently misspell.
func parseSelection(input string, options []string) (ids []string, all bool, err error) {
	if input == "" || strings.EqualFold(input, "all") {
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
	return fmt.Errorf("unknown %q (valid: %s, empty/all = all, none = drop all)",
		tok, strings.Join(numbered, ", "))
}

type keepSet struct {
	plugins  map[string]bool
	features map[string]bool
	optIn    map[string]bool
}

func parseKeep(plugins, features, optIn string) keepSet {
	k := keepSet{
		plugins:  map[string]bool{},
		features: map[string]bool{},
		optIn:    map[string]bool{},
	}
	// Empty means keep-all defaults in that dimension (the prompt asks
	// plugins and features as separate questions). Never mirror one
	// dimension into the other. The literal "none" keeps nothing.
	// Opt-in is the opposite default: empty/none keeps none, "all" keeps
	// every opt-in unit.
	if strings.TrimSpace(plugins) == "" || strings.EqualFold(strings.TrimSpace(plugins), "all") {
		plugins = strings.Join(unitIDsOf(capabilities.ScaffoldDefault, capabilities.KindPlugin), ",")
	} else if strings.TrimSpace(plugins) == keepNone {
		plugins = ""
	}
	if strings.TrimSpace(features) == "" || strings.EqualFold(strings.TrimSpace(features), "all") {
		features = strings.Join(unitIDsOf(capabilities.ScaffoldDefault, capabilities.KindFeature), ",")
	} else if strings.TrimSpace(features) == keepNone {
		features = ""
	}
	switch {
	case strings.TrimSpace(optIn) == "", strings.TrimSpace(optIn) == keepNone:
		optIn = ""
	case strings.EqualFold(strings.TrimSpace(optIn), "all"):
		optIn = strings.Join(unitIDsOf(capabilities.ScaffoldOptIn, ""), ",")
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
	for p := range strings.SplitSeq(optIn, ",") {
		if v := strings.TrimSpace(p); v != "" {
			k.optIn[v] = true
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
	return unitIDsOf(capabilities.ScaffoldDefault, kind)
}

// unitIDsOf lists manifest units in one scaffold class (empty class = any)
// and one kind (empty kind = any), in manifest order (the prompt's numbering).
func unitIDsOf(scaffold capabilities.Scaffold, kind capabilities.Kind) []string {
	var ids []string
	for _, u := range manifestUnits {
		m := u.meta()
		if scaffold != "" && m.scaffold != scaffold {
			continue
		}
		if kind != "" && m.kind != kind {
			continue
		}
		ids = append(ids, u.id)
	}
	return ids
}

// keepNone keeps nothing in a dimension (drop everything in scope).
// Empty means keep-all; unknown ids fail fast in checkKeepIDs.
const keepNone = "none"

// checkKeepIDs fails fast on unknown or misplaced unit ids: a typo like
// --plugins dagnat must error listing valid ids, never silently keep
// everything. --plugins/--features accept default units of that kind only;
// --opt-in accepts opt-in units only.
func checkKeepIDs(plugins, features, optIn string) error {
	defaultPlugins := map[string]bool{}
	defaultFeatures := map[string]bool{}
	optInIDs := map[string]bool{}
	for _, u := range manifestUnits {
		m := u.meta()
		switch m.scaffold {
		case capabilities.ScaffoldDefault:
			if m.kind == capabilities.KindPlugin {
				defaultPlugins[u.id] = true
			}
			if m.kind == capabilities.KindFeature {
				defaultFeatures[u.id] = true
			}
		case capabilities.ScaffoldOptIn:
			optInIDs[u.id] = true
		}
	}
	groups := []struct {
		raw, flag string
		valid     map[string]bool
	}{
		{plugins, "--plugins", defaultPlugins},
		{features, "--features", defaultFeatures},
		{optIn, "--opt-in", optInIDs},
	}
	var unknown []string
	seen := map[string]bool{}
	for _, g := range groups {
		for p := range strings.SplitSeq(g.raw, ",") {
			id := strings.TrimSpace(p)
			if id == "" || id == keepNone || strings.EqualFold(id, "all") || seen[id] {
				continue
			}
			seen[id] = true
			if !g.valid[id] {
				unknown = append(unknown, fmt.Sprintf("%s (not valid for %s)", id, g.flag))
			}
		}
	}
	if len(unknown) > 0 {
		sort.Strings(unknown)
		return fmt.Errorf("unknown unit id(s): %s (valid: --plugins %s; --features %s; --opt-in %s)",
			strings.Join(unknown, ", "),
			strings.Join(sortedKeys(defaultPlugins), ", "),
			strings.Join(sortedKeys(defaultFeatures), ", "),
			strings.Join(sortedKeys(optInIDs), ", "))
	}
	return nil
}

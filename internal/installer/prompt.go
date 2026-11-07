// Installer engine: interactive form, numbered menus, keep-set parsing
package installer

import (
	"bufio"
	"errors"
	"fmt"
	"io"
	"regexp"
	"strings"
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
	name  string
	owner string
}

// promptForm is the stdlib interactive form. It asks the two questions a
// scaffold needs (name, owner), in the same order a future Charm Huh form
// would ask them. (The retired keep/drop menus used to ask plugins,
// features, and opt-in units as numbered menus; dropping units at scaffold
// no longer exists, so the menus — and their parsers — are gone with it.)
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
	return res
}

// All keep/drop machinery is retired with the trim operation: scaffold
// keeps every unit, and the interactive form asks name and owner only.

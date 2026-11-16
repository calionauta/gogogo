// Installer engine: interactive form, numbered menus, keep-set parsing
package installer

import (
	"bufio"
	"errors"
	"fmt"
	"io"
	"regexp"
	"strings"

	shapekit "github.com/calionauta/gogogo/internal/shape"
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
	name    string
	owner   string
	shape   string
	recipes string
	locales string
}

// promptForm is the stdlib interactive form: the questions a scaffold needs
// that are genuinely the operator's choice. Name and owner are identity; shape
// and recipes decide what the tree IS, and a scaffold that guesses them ships a
// product nobody asked for.
//
// The defaults are the safe ones (the owner above, the full shape, every demo
// kept, English only), so pressing Enter five times still produces a working
// tree. Answering the language question is what makes the later gates grade the
// right set instead of guessing pt-BR.
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
	res.shape = ask("Product shape (full|web|api|desktop|gui|mobile)", string(shapekit.Full))
	res.recipes = ask("Demo slices to drop (none|all|<ids>)", recipesNone)
	res.locales = ask("Languages to ship, comma-separated (English is always included)", "en")
	return res
}

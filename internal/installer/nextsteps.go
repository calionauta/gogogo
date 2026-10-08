// SCOPE:layer=infra,removal=plugin — installer engine: post-scaffold next steps
package installer

import (
	"fmt"
	"io"
)

// buildNextSteps describes what to do after a successful scaffold. dropped
// decides which destinations are real: advertising the DagNats console on a
// checkout whose dagnats unit was just trimmed sends the user to a route that
// does not exist. An empty/zero Flows means "no console", and printNextSteps
// omits the line rather than printing a broken URL.
func buildNextSteps(dir string, dropped []trimUnit) nextSteps {
	n := nextSteps{
		Dir:   dir,
		Dev:   "cd " + dir + " && make dev",
		App:   "http://localhost:8080 (PORT overrides)",
		Todo:  "http://localhost:8080/todo",
		Login: "demo1@demo.app and demo2@demo.app / demo1234456 (demo1 prefilled; open a second browser for contention)",
		Admin: "http://localhost:8080/_/ (PocketBase — create the superuser on first visit)",
		Flows: "http://localhost:8080/dagnats/ (DagNats console)",
	}
	for _, u := range dropped {
		if u.id == unitDagnats {
			n.Flows = ""
		}
	}
	return n
}

func printNextSteps(w io.Writer, dir string, dropped []trimUnit) {
	n := buildNextSteps(dir, dropped)
	fmt.Fprintln(w, "gogogo: done — next:")
	fmt.Fprintf(w, "  %s\n", n.Dev)
	fmt.Fprintf(w, "  app:       %s\n", n.App)
	fmt.Fprintf(w, "  login:     %s\n", n.Login)
	fmt.Fprintf(w, "  admin:     %s\n", n.Admin)
	// Only advertise a console that still exists.
	if n.Flows != "" {
		fmt.Fprintf(w, "  workflows: %s\n", n.Flows)
	}
}

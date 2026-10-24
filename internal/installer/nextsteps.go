// SCOPE:plugin — installer engine: post-scaffold next steps
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
// emailNextSteps is the handoff for the flows that need a delivered link.
// It names the isolation rule explicitly: the age file is scoped by APP_NAME,
// so a scaffolded project must never load the template author's secrets.
const emailNextSteps = "set APP_URL + SMTP_ADDR/SMTP_FROM as environment variables; " +
	"keep SMTP_USERNAME/SMTP_PASSWORD in ~/.secrets/<your-app>.env (then bin/init-secrets --reencrypt). " +
	"Never reuse another project's secrets file; `make mailpit` is local capture only."

func buildNextSteps(dir string, dropped []trimUnit) nextSteps {
	n := nextSteps{
		Dir:   dir,
		Dev:   "cd " + dir + " && make dev",
		App:   "http://localhost:8080 (PORT overrides)",
		Todo:  "http://localhost:8080/todo",
		Login: "demo1@demo.app and demo2@demo.app / demo1234456 (demo1 prefilled; open a second browser for contention)",
		Admin: "http://localhost:8080/config (admin-only; sign in as an admin)",
		Flows: "http://localhost:8080/dagnats/ (DagNats console; needs ADMIN_EMAILS)",
		Email: emailNextSteps,
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
	fmt.Fprintf(w, "  email:     %s\n", n.Email)
	// Only advertise a console that still exists.
	if n.Flows != "" {
		fmt.Fprintf(w, "  workflows: %s\n", n.Flows)
	}
}

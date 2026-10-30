// SCOPE:plugin — installer engine: CLI help text (kept out of run.go for the
// file-sizes gate).
package installer

import (
	"flag"
	"fmt"
	"io"
)

func PrintUsage(w io.Writer, fs *flag.FlagSet) {
	fmt.Fprintln(w, `gogogo `+Version+` — scaffold a project from gogogo.

  Interactive (humans: 5 questions, then a plan to confirm):

    go run github.com/calionauta/gogogo/cmd/gogogo@latest --run

  Interactive without takeover (prints cd <dir> && make dev at the end):

    go run github.com/calionauta/gogogo/cmd/gogogo@latest

  Scripted (agents: always pin --yes; preview with --dry-run first):

    go run ./cmd/gogogo --name my-app --owner myorg \
      --plugins dagnats,whiteboard --features landing,config-view \
      --no-tui --dry-run --format json --dir ./my-app
    go run ./cmd/gogogo --name my-app --no-tui --yes --dir ./my-app

  Drift gate (maintainers/CI): verify every marker against a pristine
  checkout without changing anything:

    go run ./cmd/gogogo --check --dir ./my-app

  Add a unit to an existing scaffolded checkout (deps cascade automatically):

    go run ./cmd/gogogo add whiteboard --from ~/gogogo --dir ./my-app --yes

  Take a unit INTO an app that is not gogogo — read-only: writes a scratch
  bundle plus PORTING.md, and never touches the target tree:

    go run ./cmd/gogogo export whiteboard --dir ~/gogogo \
      --to /tmp/port-whiteboard --module example.com/my-app

  Scaffold one new feature inside a checkout (handler + page + wiring +
  capability unit + RBAC rows, then proof: templ, docs, gates, build):

    go run ./cmd/gogogo new feature reports --dir ./my-app --yes

  Opinions, not changes (for LLMs deciding what to use — reads nothing, changes nothing):

    go run ./cmd/gogogo advise --need "offline-first todo with AI" --format json
    go run ./cmd/gogogo advise --intent update --dir ./my-app

  Is this current? Two read-only checks — the tool, and the project:

    go run ./cmd/gogogo --check-update --dir ./my-app
    go run ./cmd/gogogo migrate --dir ./my-app

  --check-update compares the binary AND the revision your AGENTS.md records
  (scaffolded-from:) against the latest release; exit 3 means an update
  exists. migrate lists every release between that base and latest, and the
  gates that prove a migrated tree conforms. Neither one changes anything and
  neither runs on its own: no background check, no nag.

  Strategy: empty --need returns the full map (presets + every
  capability); a need that matches nothing prints the preset names to
  retry with — or decide straight from the capabilities table. --intent
  (update|port|add-unit|new-feature|scaffold) prints the ordered commands
  for that goal; omitted, it is inferred from --need and the answer says
  which. A --dir that is not a gogogo checkout answers with the copy
  surface and the export route instead of "cannot".

  What it does, in order:
    1. shows the trim plan with every consequence (never silent),
    2. clones the template into --dir when it is missing (with consent),
    3. deletes skipped plugins/features with their wiring calls,
    4. renames the module path + every reference (rename-project.py rules),
    5. writes AGENTS.md with the upstream-first rule,
    6. proves it: templ generate (when .templ edited) + go mod tidy +
       go build ./cmd/web, then prints the exact next commands
       (cd <dir> && make dev — a child process cannot cd its parent).
       With --run it skips the printing and becomes dev itself: execs
       make dev in the new checkout (humans only; agents must not).

  Units (id, kind). Kind follows the servant principle, not the directory:
  a plugin serves other capabilities (sounds, credits serve pages);
  a feature is a terminal user surface (todo, whiteboard, landing, config).
  Scaffold class decides the default: --plugins/--features keep default
  units (empty or all keeps all, none drops all); --opt-in adds opt-in units
  (empty keeps none, all adds every one; this tree has none). Not offered here
  todo (reference implementation), auth (middleware is core), queue (core),
  nats (runtime: NATS_ENABLED=false), llm (runtime: unset GOAI_API_KEY),
  offline-sync (runtime: OFFLINE_SYNC_ENABLED=false).`)
	fmt.Fprintln(w, "\n  Trim units:")
	for _, u := range Units() {
		fmt.Fprintf(w, "    %-12s %-7s %s\n", u.ID, u.Kind, u.Summary)
	}
	fmt.Fprintln(w, `
  Flags:`)
	fs.SetOutput(w)
	fs.PrintDefaults()
	fmt.Fprintln(w, `
  Agent contract: --format json emits stable field names.
  --dry-run prints the plan; apply prints one envelope
  {plan, receipt{units[{id, dirsRemoved, filesRemoved,
  stripsApplied, stripsMissed}]}, buildOk, buildError,
  next{dir, dev, app, todo, login, admin, workflows}}.
  Exit codes: 0 ok/plan-only, 1 usage or apply error, 2 proof build failed,
  3 an update exists (--check-update only).`)
}

// Installer engine: CLI help text (kept out of run.go for the
// file-sizes gate).
package installer

import (
	"flag"
	"fmt"
	"io"
)

// printBuildGuidance keeps the shared guidance blocks out of the
// already-long PrintUsage body (funlen).
func printBuildGuidance(w io.Writer) {
	fmt.Fprintln(w, "\n"+SyncPrerequisites)
	fmt.Fprintln(w, "\n"+DesktopPrerequisites)
	fmt.Fprintln(w, "\n"+GuiPrerequisites)
	fmt.Fprintln(w, "\n"+MygoPrerequisites)
	fmt.Fprintln(w, "\n"+MobilePrerequisites)
}

func printPreamble(w io.Writer) {
	fmt.Fprintln(w, `gogogo `+Version+` — scaffold a project from gogogo.

  Preflight: go run ./cmd/gogogo doctor --json
  Codegen: go run ./cmd/gogogo generate [--check]
`)
}

func PrintUsage(w io.Writer, fs *flag.FlagSet) {
	printPreamble(w)
	fmt.Fprintln(w, `
  Interactive (humans: 2 questions — name, owner — then a plan to confirm):

    go run github.com/calionauta/gogogo/cmd/gogogo@latest --run

  Interactive without takeover (prints cd <dir> && make dev at the end):

    go run github.com/calionauta/gogogo/cmd/gogogo@latest

  Scripted (agents: always pin --yes; preview with --dry-run first):

    go run ./cmd/gogogo --name my-app --owner myorg \
      --no-tui --dry-run --format json --dir ./my-app
    go run ./cmd/gogogo --name my-app --no-tui --yes --dir ./my-app

  Take a unit INTO an app that is not gogogo — read-only: writes a scratch
  bundle plus PORTING.md, and never touches the target tree:

    go run ./cmd/gogogo export whiteboard --dir ~/gogogo \
      --to /tmp/port-whiteboard --module example.com/my-app

  Scaffold one new feature inside a checkout (handler + page + wiring +
  capability unit + RBAC rows, then proof: templ, docs, gates, build):

    go run ./cmd/gogogo new feature reports --dir ./my-app --yes

  Install a recipe (a file set plus .recipe.yml) into a checkout. Without
  --yes it prints the plan and changes nothing; --yes copies the tree, writes
  .recipes/<id>.yml and regenerates internal/server/recipes.gen.go:

    go run ./cmd/gogogo recipe install ./extensions/pomodoro --dir ./my-app
    go run ./cmd/gogogo recipe install ./extensions/pomodoro --dir ./my-app --yes
    go run ./cmd/gogogo recipe install --entry catalog/pomodoro/v1.2.0.yml \
      ./downloaded/pomodoro --dir ./my-app --yes
    go run ./cmd/gogogo recipe update ./extensions/pomodoro --dir ./my-app --yes
    go run ./cmd/gogogo recipe uninstall pomodoro --dir ./my-app --yes

  What did I change inside a recipe? Pristine vs yours, plus whether the
  receipt still matches the copied code (read-only):

    go run ./cmd/gogogo recipe status --dir ./my-app

  Freshness of an installed recipe — version verdict against the reviewed
  catalog (catalog/<id>/<version>.yaml; --catalog names another directory).
  Exit 3 means an outdated recipe exists (a verdict, not an error):

    go run ./cmd/gogogo recipe outdated --dir ./my-app

  Discover recipes headlessly (CLI and MCP read the same registry + receipts):

    go run ./cmd/gogogo recipe list --dir ./my-app --format json
    go run ./cmd/gogogo recipe describe pomodoro --dir ./my-app --format json

  Invoke one declared verb against a running server (loopback only):

    gogogo recipe invoke todo create --server http://127.0.0.1:18099 \
        --session "$GOGOGO_SESSION" --param title="Ship it"

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
  (update|port|new-feature|scaffold) prints the ordered commands
  for that goal; omitted, it is inferred from --need and the answer says
  which. A --dir that is not a gogogo checkout answers with the copy
  surface and the export route instead of "cannot".

  What it does, in order:
    1. shows the plan with every consequence (never silent),
    2. clones the template into --dir when it is missing (with consent),
    3. renames the module path + every reference (rename-project.py rules),
    4. writes AGENTS.md with the upstream-first rule,
    5. proves: templ generate (when .templ edited) + go mod tidy +
       go build ./cmd/web, then prints the exact next commands
       (cd <dir> && make dev — a child process cannot cd its parent).
       With --run it skips the printing and becomes dev itself: execs
       make dev in the new checkout (humans only; agents must not).

  Capabilities (id, kind). plugin (site: Servant) serves other
  capabilities; feature (site: Recipe) is a terminal user surface. core
  is load-bearing, including the recipe engine. Every capability ships
  in the scaffold; manual removal is recorded per-unit Reason.`)
	printUsageTail(w, fs)
}

// printUsageTail renders the reference half — build guidance, the unit table, the
// flags and the agent contract. Split from PrintUsage for the funlen gate: it is
// what a reader consults after deciding on a command.
func printUsageTail(w io.Writer, fs *flag.FlagSet) {
	printBuildGuidance(w)
	fmt.Fprintln(w, "\n  Capabilities:")
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
  3 an update exists (--check-update and recipe outdated only).`)
}

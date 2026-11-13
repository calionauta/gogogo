# App shapes: one template, five products

Reviewed: 2026-11-12

**Question.** A user of `gogogo new` may want only the web app, only an
API/backend with no UI, only the desktop shell, or all of them. The scaffold
today ships the whole tree and offers no shape choice (`docs/research/scaffold-choice-2026-10.md`
retired the keep/drop prompt). How should shapes be expressed, applied and
gated without resurrecting a second registry?

**Short answer.** A shape is a *closed enum* that selects which **entrypoints**
and which **capabilities** the tree ships, declared once in
`internal/capabilities`, recorded in the generated `AGENTS.md` provenance block
and in a `.gogogo/shape.yml`, applied by the existing unit-removal mechanics,
and proved by a `shape check` subcommand (proposed). It is not a new prompt: the CLI, the
MCP tool and the docs all read the same enum.

## Why the current scaffold cannot answer it

Scaffold ships every capability by design: `capabilities.All` is the source of
truth and `buildPlan` lists it whole. The removal mechanics still exist
(`internal/installer/manifest.go`, `builtinUnits`, the exporter), but nothing
asks the question. A shape is that question with a machine-readable answer.

## The enum

| Shape | Entrypoints | UI (`internal/ui`, `features/*/components`) | Capabilities |
|---|---|---|---|
| `web` | `cmd/web` | yes | all |
| `api` | `cmd/web` (API-only boot) | no | core + servants; no demo features |
| `desktop` | `cmd/desktop` (+ `cmd/web` as its server) | yes | all |
| `gui` | `cmd/gui` | native (gogpu/ui) | core + the slices the panel reuses |
| `mobile` | `cmd/mobile` | yes (Wails) | core + a small set |
| `full` | every target | yes | all (today's behaviour, the default) |

Two rules keep the table honest:

- **A shape never invents a boot path.** `api` means `cmd/web` serves
  `/api/*` and health, and every page route is absent — not a second server
  binary. That is the only shape whose cost is real (the UI shell, the page
  handlers and their RBAC rows must be trimmed coherently), so it lands last.
- **A shape is a prefix of `full`.** Everything it keeps must build and pass
  the same gates; nothing a shape drops may be imported by something it keeps.
  `cmd/check-flatdeps` already enforces the second half for packages.

## Where it lives

1. `internal/capabilities`: add `Shapes []string` to `Capability` (empty means
   "every shape"), and a `capabilities.Shapes` enum with `Parse`/`Valid`. The
   registry coverage test already fails on a package no row owns, so a new
   unit must declare its shapes.
2. `internal/installer`: `--shape` on `new` (default `full`), echoed in the
   plan JSON (`"shape"`), the envelope and the interactive prompt. `--shape`
   plus `--plugins`/`--features` stays possible, but the shape sets the
   default set and the plan prints what the shape removed.
3. `cmd/gogogo-mcp`: the same flag on `scaffold` (one engine, two transports —
   `internal/installer` owns both, so this is a schema change, not logic).
4. The scaffold writes `.gogogo/shape.yml`:

   ```yaml
   shape: api
   entrypoints: [cmd/web]
   removed: [desktop, gui, mobile, landing, whiteboard, notes, room, review]
   ```

   It is the machine-read record, the way `scaffolded-from:` is today. A tree
   with no file is `full`.

## The gate

a `shape check --dir .` subcommand (proposed; also `make check-shape`, cheap, text + one build):

- the file parses and names a known shape;
- every path the shape says it removes is absent, and every entrypoint it says
  it keeps exists;
- the capability registry's conformance test runs with the shape's removals
  skipped (the same mechanism `removed-paths:` already uses);
- `go build ./cmd/web` (and the shape's other entrypoints) passes.

## Phases

1. **Enum + record (web/api/full).** Registry field, `--shape`, plan/envelope,
   `.gogogo/shape.yml`, `shape check` for `full` and `web` (both keep every
   unit, so the gate is trivially green and the plumbing is proven).
2. **Desktop/gui/mobile.** These are file-set drops only (the entrypoints and
   their Makefile targets); no page changes. Apply with the existing trim
   units, prove with the per-target manual builds the skills already require.
3. **API shape.** The only expensive one: introduce a `server.PageRoutes`
   seam so `api` mounts none of the page handlers, keep `/api/*`, `/healthz`
   and the session/RBAC edge wrappers, and assert in a test that a page route
   answers 404 in `api` and 200 in `web`.
4. **Docs and the MCP schema.** `docs/getting-started.md` shows the choice;
   `docs/unit-contracts.md` (generated) gains a shape column.

**Not in scope:** a `library`/`embedded` shape (no HTTP at all). It needs a
different composition root, and the native panels already prove the
"render the page's components" path separately.

## Decision

Do it as a closed enum applied through the existing removal mechanics, in the
phase order above. Do not reintroduce a per-unit checkbox prompt: the failure
mode that retired it (three lists disagreeing about what ships) comes back the
moment the shape choice is not one list.

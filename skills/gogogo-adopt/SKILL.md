---
name: gogogo-adopt
description: "Decide whether gogogo is the right base, and adopt it without guessing. Triggers when: the user asks for a new system, product, app or internal tool to be built; asks whether to use gogogo, what stack to start from, or how to adopt it in an existing app; or has the gogogo MCP/CLI available and no checkout yet. Covers the one question that must be asked before any mutation, what gogogo is and is NOT for (a static or conversion page is another tool), MCP vs CLI, and where the project's own skills come from after scaffolding."
---

# Adopting gogogo

This skill is the **entry point**: what gogogo is for, and the order to run it
in. The three sibling skills own everything after the tree exists —
`gogogo-coding-standards` (Go/template rules), `gogogo-ui` (DaisyUI, Templ,
signals), `gogogo-recipe-authoring` (the copy-and-evolve slice contract).

## The rule that matters

**Never scaffold a product without asking the operator first.** Name, owner
(GitHub org/user) and the languages the product ships are the operator's
decisions, and a wrong one becomes a Go module path, a repo and a rename
problem. Preview, then ask, then apply:

```
advise_stack   need=<what the user wants in their words>
scaffold_plan  name=<proposal>, owner=<proposal>, dir=<path>, shape=<full|web|api|desktop|gui|mobile>, recipes=<none|all|ids>
               → returns the plan. Nothing is written.
            ← ASK THE USER: name, owner, dir, shape, languages. Quote the plan's
              dropped slices and the kept ones. Do not decide for them.
scaffold_apply name=…, owner=…, dir=…, confirm=true
               → clones, renames, trims, proves the build.
```

`confirm:true` is a guard against an accidental write, **not** a substitute for
asking. The same order applies to `new_feature` (preview with `dryRun`) and to
every `recipe_install` / `recipe_uninstall`.

Record the languages once, before scaffolding slices:
`locales` (MCP) or `gogogo locales --set en,pt-BR` (CLI). `new feature`,
`recipe check` and `make check-locales` all grade against that answer.

## What gogogo is for — and what it is not

It is a **product foundation**: one Go binary with a database, owned auth,
background jobs, realtime (SSE), durable workflows, an admin surface and a
private app area behind a session. Fit it when the user is building something
**people sign into**: an internal tool, a SaaS backend, a dashboard, a
collaborative app, an API with accounts.

It is **not** a static-site or landing-page generator. `GET /` is a redirect:
a guest goes to `/login` (see `internal/server/home.go`), and the tree ships no
marketing page on purpose. So:

| The ask | Answer |
|---|---|
| "a landing page", "a one-page promo", "a conversion page" | Another tool. gogogo has no page to fill in. |
| "a static site", "a blog with no accounts" | Another tool (or `marketing-site` + `content-model` presets if an app is coming behind it). |
| "an app with a login", "a dashboard", "a product", "a SaaS", "an internal tool" | gogogo. |
| "a landing page **and** the app behind it" | gogogo for the app; the public page is yours to write, and it is not a unit. |

The distinction is **shape, not vocabulary**: accounts and state behind a
session is a product; a page that only converts is not. Do not refuse a need
because it used the word "site", and do not scaffold one because it used the
word "app".

## MCP or CLI

Same engine (`internal/installer`), same plans, same proofs. Pick by what the
agent has:

- **MCP (`gogogo-mcp`, stdio)** — the agent calls tools directly, no shell
  quoting, structured JSON back. Best first step when nothing is on disk:
  `advise_stack` answers "does this fit?" from any directory, and
  `capabilities_list` / `recipe_describe` / `recipe_source` let the agent read
  before it writes. Wire it per client (Claude Code `.mcp.json`, Codex
  `~/.codex/config.toml` `[mcp_servers.gogogo]`, `opencode mcp add gogogo -- …`,
  `pi mcp add gogogo -- …`); see `docs/mcp.md`.
- **CLI (`gogogo`)** — one verb, scriptable, same output with
  `--format json`. Use it when the agent has a shell and prefers one command to
  a tool call, or when a human is typing.

Either way the verbs are the same names: `advise` ↔ `advise_stack`,
`--dry-run` ↔ `scaffold_plan`, `--yes` ↔ `confirm:true`.

## Scope rails (for gateways and shared machines)

The operator — not the model — decides how much power the tools have, by
environment variable:

- `GOGOGO_READONLY=1` — every mutation refuses; `advise`, plans, `check` and
  `doctor` keep working. Use it when the agent should advise but not build.
- `GOGOGO_ALLOWED_ROOTS=/abs/path[:/abs/path2]` — confines `--dir`/`--from` to
  those roots.

Both are unset by default, which is local-CLI behavior.

## Already have an app? Port, do not scaffold

Nothing installs into a tree gogogo did not create. The read-only path is:

1. `advise_stack` with `dir=<their repo>` → the `foreign-tree` scope: the copy
   surface (owned paths per unit) plus the route.
2. `export_unit unit=<id> dir=<template> to=<scratch> module=<their module>` —
   writes a bundle with the files, the exact wiring, the import rewrites and
   the proof into a scratch directory. It never touches their tree.
3. The wiring is theirs to write: `docs/porting-into-an-existing-app.md`.

`recipe source <id>` reads a slice's code with no receipt and no copy — use it
when the user wants the pattern, not the files.

## After scaffolding: the project's skills

The new tree carries `skills/` (renamed: `<project>-coding-standards`, plus
`gogogo-ui`, `gogogo-adopt`, and the vendored pins) but **the scaffold does not
re-register them**. A host loads a skill by name from `.agents/skills/<name>`;
if the project has no such link, the agent sees prose at best.

So after `scaffold_apply`, either:

- `npx skills add <owner>/<repo>` — installs the skills the host understands, or
- link one by hand: `ln -sfn ../../skills/<name> .agents/skills/<name>` (keep
  the skill directory and the link name identical, or `cmd/check-skill-frontmatter`
  fails).

Say this out loud once per project; it is the difference between an agent that
follows the house rules and one that improvises.

## Proof, then report

A scaffold is not done when the files exist. Read what the tool proved:
`gogogo --check` (manifest), the build output from `scaffold_apply`, and
`recipe check <id>` for each slice. Report what passed — never claim a build
you did not see.

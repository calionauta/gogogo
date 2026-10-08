# Contributing to gogogo

Thank you for considering a contribution. Right now the most valuable one is
**using gogogo for a real project and reporting what broke** — friction from
real usage (scaffold, rename, deploy, confusing docs, upgrade pain) is worth
more to this template than speculative features.

AI-assisted contributions are welcome, with a human in the loop. What matters
is not how you wrote the change, but whether you understand it, tested it, and
can defend it in review.

## What helps most, in order

1. **Use it.** Scaffold a real app (`install.sh --run` or `go run ./cmd/gogogo --run`),
   build something small, note every point of friction.
2. **Report friction with a reproduction.** A precise bug report with
   expected-vs-actual, environment, and minimal reproduction steps is worth more
   than most PRs. See [Issues](#issues) below.
3. **Fix docs and small papercuts.** Unclear explanations, broken examples,
   misleading error messages.
4. **Small, focused code PRs** linked to a discussed issue. One concern per PR.

Please discuss a feature in an issue *before* opening a PR. Unsolicited feature
PRs with no prior discussion will be redirected to an issue first — not out of
hostility, but because reviewing a diff for a problem nobody confirmed is the
most expensive work a maintainer does.

## AI policy: allowed, disclosed, human-owned

- **Allowed.** Use any assistant, agent, or autocomplete you like.
- **Human in the loop, always.** You must read and review every line you submit,
  run it end to end, and be able to answer questions about it in review. Never
  be in the position of saying "I don't know, the model did it." Unattended bots
  that open or comment on issues/PRs without your approval are not allowed.
- **Disclose in the PR body, not in commits.** Write one line in the pull
  request description: which tool you used and what it did, e.g.
  "AI assistance: Claude Code drafted the handler and tests; I reviewed,
  trimmed, and verified them." This repo does **not** use AI attribution in
  commit messages, release notes, or changelogs (no `Co-Authored-By:` /
  `Assisted-by:` / `Generated-by:` trailers, no AI mentions in `CHANGELOG.md`) —
  keep provenance in the PR description, where reviewers see it.
- **Write the PR description yourself** (translation or copy-editing help is
  fine). It must explain motivation, approach, impact, and open questions to
  the same standard as a fully human-written change.
- **`good first issues` are for humans learning.** Do not automate them away
  with an agent end to end; reviewers will deprioritize PRs that do.
- **You own licensing.** Do not submit output you have reason to believe
  reproduces copyrighted or incompatibly licensed material. Regenerating it
  with a tool does not clear it.

## Issues

Search [open issues](https://github.com/calionauta/gogogo/issues) first — yours
may already exist. A good issue contains information the maintainer could *not*
have produced alone in fifteen minutes:

- **Motivation.** What you were building, and what blocked you.
- **Expected vs. actual behavior.**
- **Environment.** OS, Go version, how you scaffolded (`install.sh` vs manual),
  relevant env vars.
- **Minimal reproduction.** Steps, commands, or a small script that shows the
  failure. For bugs, the red test or log excerpt matters more than the theory.
- **What you already tried.** If you pointed an agent at the codebase, include
  what it attempted and what you learned — that is the information the
  maintainer actually needs.

Feature requests without a use case, and bug reports without a reproduction,
will be asked for one or closed. This keeps the queue reviewable.

## Pull requests

1. **Coordinate first.** Comment on the issue, wait for a maintainer nod, check
   for overlapping open PRs. If your approach differs materially from an
   existing PR, say so in the issue before opening a second one.
2. **Keep it minimal.** One concern per PR. No unrelated files, no drive-by
   refactors, no AI comment bloat. If it can be a 20-line fix, make it a
   20-line fix. Small PRs get reviewed; large ones wait.
3. **Follow the repo's working rules** ([AGENTS.md](AGENTS.md) is normative for
   agents; the same rules apply to you):
   - `// SCOPE:layer=…,removal=…` on every new non-test, non-generated `.go`
     file under `internal/`/`features/` (`make check-scope` enforces it).
   - `make templ && make css` after `.templ` edits; never hand-edit generated
     `_templ.go` (`make check-generated`).
   - No real LLM in tests — inject a stub (`internal/llm/fakeserver` only
     inside `internal/llm/`).
   - Ephemeral ports in tests (`127.0.0.1:0`, NATS `-1`); never fixed ports.
   - `GOGOGO_NO_BROWSER=1` in the child env when a test spawns the real
     binary (built from scratch, not appended to `os.Environ()`).
   - Go-first: no new native/Zig code without a benchmarked case
     ([exception-to-go](docs/exception-to-go.md)).
   - Project footguns live in `rules/rules.go` (ruleguard) — if your change
     trips one, fix the code, not the rule.
4. **Verify before asking for review.** Run `make signoff` (full local gate =
   CI: templ, datastar-lint, css-check, scope, lint, race tests, build, smoke).
   For small changes during iteration, `make ci-local-fast` scopes lint + tests
   to changed packages. PRs with red CI will not be reviewed until green.
5. **Describe the verification.** Test commands run and their result, plus the
   red→green evidence for bug fixes.

## What gets closed or redirected fast

- Duplicate PRs for an issue someone already covers.
- PRs that appear to be unreviewed agent output (author cannot explain the
  diff, CI red, no reproduction).
- One-line busywork PRs (single typo, isolated style tweak) — bundle them or
  leave them; reviewer time is the bottleneck.
- Feature PRs with no linked prior discussion.
- Anything adding AI trailers to commits or release notes (see above).

## Golden rule

A contribution should be worth more to the project than the time it takes to
review it. When in doubt, shrink the diff or strengthen the reproduction —
that is what converts a request for someone else's time into an offer that is
cheap to say yes to.

## License

MIT — see [LICENSE](LICENSE). By contributing you agree your work is offered
under the same terms.

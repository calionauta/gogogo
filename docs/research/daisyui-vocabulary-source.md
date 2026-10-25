# Where a linter's vocabulary lives, and how it stays true

**Reviewed:** 2026-10-25 · **Status:** accepted

The question is narrow and it decides the product: a UI linter needs a list of
what the library actually ships. Where does that list come from, and what keeps
it from rotting? This note compares the two strategies already in use in this
org, checks them against 2026 practice, and records the decision for
`daisyui-lint`.

## The two strategies, as built

**Authored in the tool — `datastar-lint`.** The vocabulary is a Go map written by
hand (`attrs.go: knownAttrs`), the tool declares the Datastar release it was
verified against, and a test pins the version constant to the CHANGELOG. There is
no upstream artifact consumed at build or run time and no sync job. Datastar has
~50 attributes and an API that changes slowly, so hand-authoring is affordable —
and the rules catch the mistakes that matter (unknown attribute, missing
selector, signal typo).

**Derived from a vendored artifact — `daisyui-lint`.** The vocabulary is parsed
from the vendored DaisyUI skill (`.agents/skills/daisyui/SKILL.md`, refreshed
from `daisyui.com/llms.txt`, pinned by `UPSTREAM_SHA`). 72 components, their
parts, sizes, colours and documented host elements are read out of the skill's
own `#### Class names` and `#### Syntax` blocks. Shape changes are errors, not
silent skips, and a weekly drift workflow detects upstream movement.

## What 2026 practice says

- **Hand-maintained spec constants go stale silently.** claudelint's `specdrift`
  design states the failure precisely: its constants encode an upstream spec that
  ships several times a week, "so those constants go stale silently until a
  dogfood pass or a user report catches a false positive". Its answer is drift
  detection, not more careful editing.
- **A committed generated artifact needs a regeneration check.** The 2026 default
  (Safeguard, Oct 2026) is: commit the generated output so every change is
  reviewed, and have CI prove that regeneration produces no difference.
  Otherwise the artifact freezes and the upstream rename never reaches the repo.
- **Machine-checkable contracts beat prose as a source.** The strongest statement
  of the principle (alint ADR-0001) is "generate everything downstream from one
  source; fail CI on any diff — never make English prose a regenerable source of
  truth".
- **Breaking-change detection is a gate, not a review habit.** For API-shaped
  artifacts the practice is a structural diff plus a SemVer policy.

## Applying it to DaisyUI

DaisyUI publishes **only prose** (`llms.txt` and the skill are the same text with
a stable structure). There is no JSON schema to generate from, so "never parse
prose" cannot be satisfied literally here. The strongest available version of the
principle is: acknowledge the prose source, and make every derived artifact
machine-checked.

Decision, now implemented:

1. **The skill is the upstream artifact; the repo vendors it** (byte-pinned,
   drift-checked). It is the one thing a maintainer reads.
2. **The binary embeds a generated snapshot**
   (`internal/daisyui/vocabulary.json`). A project that is not gogogo has no
   vendored skill, so without the snapshot the tool is unusable there — it used
   to exit 2. The snapshot is what makes `go install …/daisyui-lint@latest` a
   working product rather than a repo-only script.
3. **CI proves the snapshot is current.** `TestBundledVocabularyMatchesSkill`
   re-parses the skill and fails on any difference; `make vocabulary` regenerates
   it. This is the "commit it and check regeneration" rule, and the same rule
   this repo already applies to `_templ.go` and `app.min.css`.
4. **`--skill` overrides the snapshot**, so a project can pin its own copy and
   lint against the DaisyUI it actually installed.
5. **The source is disclosed.** `daisyui-lint --version` prints `source: bundled`
   or the skill path plus the DaisyUI release the vocabulary came from: a run
   against a different vocabulary must not look identical to a run against the
   tree's own.
6. **No runtime fetch.** The network is not part of the gate (offline CI, air-gapped
   machines, and a fetch failure would turn a lint into an outage).

The residual risk is the one the principle warns about: the skill is prose, so a
prose rewrite can change the vocabulary. The parser's tripwires narrow it (a
component section that stops yielding classes is an error, and a parse below 40
components is an error), and the snapshot test catches whatever slips through on
the next regeneration.

## What this implies for `datastar-lint`

Keep the authored map — Datastar's surface is small and stable, and a generated
artifact there would add a pipeline for little gain. Add the piece that is
missing: a **periodic drift job** that extracts `data-*` names and modifiers from
Datastar's docs or release notes and diffs them against `knownAttrs`, opening an
issue on difference. Not a build failure — the source is prose, and a hard gate
on a fetch is how a linter gets disabled. The two projects then follow one
policy: the vocabulary is data derived from a pinned upstream artifact, the
derived artifact is committed *and* CI-checked, and the runtime never fetches.

## When `daisyui-lint` becomes its own repository

The extraction is already in place (`internal/daisyui` has no repo knowledge) and
the binary is installable today. The trigger is a real second consumer, not a
hypothetical one: the first outside user asking for a fix, or a second repo of
ours using it. At that point the snapshot moves with the package, the skill is
vendored in the lint repo (deterministic CI, no fetch), the diagnostic codes
become the versioned contract, and `--check-update` / a CHANGELOG come from the
`datastar-lint` playbook.

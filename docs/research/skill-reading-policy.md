# Skill reading policy: the whole vendored skill, or the sections you use?

**Reviewed:** 2026-10-25 · **Decision:** read the sections, not the file

## The question

`gogogo-ui` says an agent must read the vendored DaisyUI skill before any
`.templ` edit. That file is 2,316 lines / 79,723 bytes. Is the full read earning
its cost, or is the component-section read plus the linter enough?

## Method

Two isolated worktrees, same model, same task, same gates. The task added an
"Archive health" card to the account page using six structure-sensitive
components: `steps`, `chat` (with avatar), `indicator` (badge before the bell),
`join`, `radial-progress` (the `--value` custom property, not a class), and
`swap` (label + native checkbox).

| | Arm A — today's policy | Arm B — proposal |
|---|---|---|
| Skill read | whole file (2,316/2,316 lines) | `grep -n '^### '` for the index, then the 6 matching sections + Avatar/Badge |
| Skill bytes read | 79,723 (100%) | 6,846 (8.6%) |
| Other mandatory reads | ui-themes, datastar.md, themes.go (21,247 B) | same (21,247 B) |
| Gates | make templ, daisyui-lint, datastar-lint, css-check, check-classes, build | same |

## Result

| | A (full read) | B (sections) | Delta |
|---|---|---|---|
| Total tokens | 163,484 | 120,165 | **+43,319 (+36%)** |
| Tool calls | 78 | 61 | +17 |
| Wall clock | 559 s | 322 s | **+238 s (+74%)** |
| Canonical structures (6) | 6/6 | 6/6 | equal |
| daisyui-lint / datastar-lint / check-classes / check-generated / css-check | all clean | all clean | equal |

The policy-forced part of the delta is 72,877 bytes of vendored skill ≈ 18k
tokens per UI task. The rest is exploration the full-read arm chose to do (it
also read `cmd/check-classes` and the linter's own source).

Judgement details where the arms differed, none of them correctness: A coloured
both the completed and the current step (`step-primary` on Backup and Verify,
`aria-current` on Verify); B coloured only the current one. A used words inside
the swap's on/off bodies, B used glyphs. Both inlined a bell SVG because
`internal/icon` ships no bell — an equal, reviewed deviation, and the full read
did not prevent it.

## Decision

`gogogo-ui` now tells an agent to read the index of component sections and the
sections the change touches, and to treat the linter as the vocabulary
authority. The whole file is for audits and for names you cannot guess yet.

The skill's own structure supports this: it is a catalogue of per-component
sections (`### Name` → class names, syntax, rules), and the six sections this
task needed are 5.2 KB — 6.5% of the file. The linter enforces the parts that
matter for class names (`UNKNOWN_CLASS`, retired v4 names, parts and hosts), and
the sections carry the structure the linter cannot see (an `indicator-item`
must come first, a swap needs a checkbox, radial progress carries
`role`/`aria-valuenow`).

## What would change the decision

- a weaker or older model that does not already know the DaisyUI 5 vocabulary;
- a task that cannot name its components (open-ended "make this page nicer"),
  where the full catalogue is the discovery mechanism — reading the section
  index first still answers that cheaply;
- a component whose section turns out to be underspecified and sends the reader
  into the rest of the file.

One run per arm, one model: the cost direction is unambiguous (the delta is
mostly bytes), the quality equality is the part to re-check if the model or the
skill version changes.

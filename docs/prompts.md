# Prompts

> **Rule.** A model-facing instruction is a file, never a Go string. The files
> live in [`internal/prompts/files`](../internal/prompts/files), are loaded by
> [`internal/prompts`](../internal/prompts/prompts.go), and are enforced by
> `make check-prompts`.

Why: a prompt is content that changes weekly, is reviewed by people who do not
read Go, and is the unit an optimizer scores. Keeping it in a string literal
means the only ways to change, version, compare or instrument it are a code
deploy and a diff review of a handler. Keeping it in a file means the same
change is one reviewable file, addressable by id and version.

## The format

The files use the **Dotprompt** shape — YAML frontmatter between `---` fences,
template body below — as published by Google's
[dotprompt](https://github.com/google/dotprompt) project and loaded by
Genkit's prompt directory. The template adopts the *file shape*, not Genkit's
runtime; the same file remains readable by any registry or optimizer that
understands frontmatter plus `{{variable}}` templates.

```text
---
name: genui/envelope
description: "Answer envelope every JSON Chat leg asks for, with the advertised component vocabulary spliced in."
version: 1
output: json
variables: [components]
---
Answer with a single JSON object shaped {"components":[{"type":"<name>","props":{...}}]} using ONLY these components: {{components}}
```

| Field | Required | Meaning |
|---|---|---|
| `name` | yes | Must equal the path inside `files/` without `.prompt` (`genui/envelope`). A renamed file cannot keep advertising the old id. |
| `description` | yes | One sentence: when this prompt is used. |
| `version` | yes | Integer, `>= 1`. Bump it when an evaluation result would no longer be comparable (see [Instrumentation](#instrumentation)). |
| `output` | no | `json` or `text`. What the caller parses. |
| `model` | no | Optional pin. Empty means the model the app is configured with, which is the norm here. |
| `variables` | yes | The `{{name}}` placeholders the body uses, exactly. An undeclared use and an unused declaration both fail the gate. |

Only `{{variable}}` substitution is supported. That is deliberate: a prompt
that needs arithmetic is a prompt doing a job the caller should do, and a
renderer with a full template language cannot be read by the tools we want to
point at these files.

## Layout

```
internal/prompts/files/
  chat/        suggest-todo, router-instructions
  genui/       answer envelope, guidance fragments, rules, data-context
  panels/      one line per system panel (what the model is told the panel is for)
  review/      maker and critic, per tab
  installer/   the AGENTS.md written into a scaffolded project
```

One directory per domain, one file per call site or per instruction. Name for
the decision the file makes (`genui/rule-data`), not for the sentence it
contains. When a prompt has conditional parts, keep the *composition* in Go and
the *prose* in files: `prompts.go` decides which files participate,
`joinFragments` assembles them, and no sentence lives in the code.

### Adding a prompt

1. Write `internal/prompts/files/<domain>/<name>.prompt` with the frontmatter
   above. `version: 1`, `variables: []` when it is pure prose.
2. Load it at exactly one call site:

   ```go
   // pure prose
   return prompts.Text("panels/todo-list")

   // with data
   return prompts.MustRender("genui/lead", map[string]string{"component": name})
   ```

   `Text`/`MustRender` panic only when a file is malformed, which the gate and
   the package tests catch before the binary ships. `Render` returns an error
   when a caller is passing variables the file does not declare.
3. Run `make check-prompts` and the package tests:
   `go test ./internal/prompts ./features/genui/components`.
4. If the prompt's behavior is pinned by a test (it should be — a prompt rule
   with no assertion is a comment), keep the assertion and confirm it still
   passes. Prompt text moving into a file must not change the wire behaviour.

`gogogo new feature <name>` follows this by construction: it writes
`internal/prompts/files/panels/<name>.prompt`, the generated panel and the
generated declaration both load it by id, and the scaffold's proof runs
`check-prompts` — so a scaffolded feature cannot reintroduce the inline pattern
the gate forbids.

## The gate

`go run ./cmd/check-prompts` (also `make check-prompts`, the pre-commit
`prompts` job, and the CI checks job) enforces four invariants:

1. **Every file is valid.** Frontmatter name matches its path, description and
   version are present, `output` is `json`/`text`, declared variables and the
   body agree, and no unsupported `{{construct}}` is left.
2. **No prompt in Go.** A string literal (or a `+` chain of literals) that is
   at least 120 characters, contains at least three distinct instruction
   markers (`you are`, `return only`, `reply with`, `must `, …) and at least
   two sentence boundaries fails. The heuristic is conservative on purpose: an
   error message, a log line, or a Go source template cannot reach all three
   thresholds by accident.
3. **No declared prompt in Go.** A `Prompt:` field holding a literal fails
   whatever its length — a panel's sentence is model-facing by construction, so
   the field name is the rule and the length never matters. This is the rule that
   catches the short ones the heuristic above cannot see.
4. **No dead prompt file.** A file no code loads fails: it is a second copy of
   a prompt waiting to drift, or the residue of a migration that left the old
   string in the code. Renaming a prompt without updating its reader is caught
   here.

A deliberate exception states a reason on the line above:

```go
// allow-inline-prompt: Go source template for a scaffolded recipe (codegen), not model instructions
const newRecipeTemplate = `…`
```

A marker with no reason after the colon is not an exemption — the gate rejects
it, so the escape hatch cannot be used to silence a finding in passing.

## Instrumentation

Every prompt is addressable as `id` plus `version`:

```go
p, err := prompts.Get("review/critic-code") // p.ID, p.Version
```

That is the two-field handle an observability backend needs to group traces by
prompt revision, which is what makes "the new prompt is worse on these inputs"
answerable. Logging a prompt by id/version is the supported move; logging its
**text** is not, because the rendered text can carry the user's data.

A composed prompt is several files, so the trace records the SET:
`features/genui/components` builds it with `prompts.Composer`, and the model leg
emits one `genui: prompt` line carrying

- `prompt.parts` — the file ids, in composition order;
- `prompt.parts.versioned` — the same ids as `id@version`, which is what an
  evaluation groups by.

`ChatSuggest` does the same with `prompts.RenderMeta` for its single file. A
prompt text change therefore moves the version field, never the log shape.

The intended next step is the OpenTelemetry GenAI semantic conventions
(`gen_ai.prompt.name`, `gen_ai.prompt.version`) once the template grows an OTel
export path; the file ids are already shaped for it.

## Optimizing prompts (GEPA and friends)

[GEPA](https://github.com/gepa-ai/gepa) rewrites a prompt, scores the rewrites
against a task, and keeps the winners. It needs three things this layout
already provides:

- **a prompt as text, addressable outside the program** — the `.prompt` body;
- **an evaluator** — a Go test or fixture set that exercises the call site (the
  existing `*_test.go` files around genui, review and the installer);
- **a place to put the winner** — a file diff, reviewed like any other.

### The override seam

A candidate tree can shadow the embedded library without a rebuild:

```bash
GOGOGO_PROMPTS_DIR=/tmp/candidate make dev        # or go test ./features/genui/...
```

`internal/prompts` reads every `Get` through that directory first and falls back
to the embedded copy when the id has no file there, so a candidate tree may
hold only the prompts it changes. Three properties make it usable in a loop:

- **Read fresh per call.** No cache in front of the override, so an optimizer
  can rewrite a body between two runs of the same binary and see the change.
- **Contained.** The read goes through `os.Root`, so an id that walks up the
  tree (`../…`) is an error rather than a file above the directory.
- **Same contract.** A candidate file is parsed by the same `Parse`/`Validate`
  the embedded tree uses: a name that disagrees with its id, an undeclared
  variable or a malformed fence fails the load instead of reaching a model.
  Because `MustRender` panics on a malformed prompt, a bad candidate fails the
  request loudly — which is what you want while scoring, and why the variable
  is unset in every shipped configuration.

Unset (the default everywhere outside an experiment) means the embedded tree
alone. `make check-prompts` validates the embedded tree, because that is what
ships — and when `GOGOGO_PROMPTS_DIR` is set it gates the candidate tree too:
every file must parse, name an id the library already has, declare the same
`variables`, and carry a higher `version` when the body differs. That last rule
is the one that keeps an optimizer honest: a rewrite under the old version
makes its score incomparable with the run it is supposed to beat.

The workflow:

1. Copy the file to a scratch directory and let the optimizer rewrite the body
   (keep the frontmatter; `variables` must not change or the gate will fail).
2. Score both versions against the fixture set.
3. Land the winner as a normal commit with `version` bumped, and record the
   comparison in a `docs/research/` note if the decision is not obvious.

Two rules keep this honest: never optimize against the tests' exact strings
(that optimizes the test, not the task), and never let an optimizer's output
skip review — the file is in the tree, so it gets the same treatment as code.

## Why not the alternatives

| Option | Why not |
|---|---|
| A prompt registry service (Langfuse, PromptLayer, …) | Adds a network dependency and a second source of truth for text that ships inside the binary. Revisit when a non-engineer must edit prompts in production; the `.prompt` shape is exportable to those tools then. |
| JSONL fixtures | Fine for evaluation inputs, wrong for prompts: no prose readability, no diffable body, no frontmatter. |
| `prompts.json` (one file) | Every change touches every prompt; diffs collide and review degrades. |
| Plain `.md` | No machine-readable variables/output, so the loader and the gate would have to guess. |

The choice follows the 2026 consensus for small teams: prompts in Git, one
file per prompt, frontmatter for metadata, evals in CI — and a migration path
to a registry only when someone who cannot open a PR needs an editor.

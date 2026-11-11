# Headless CMS in Go: what the ecosystem has, and what fits gogogo

Reviewed: 2026-11-11

**Question.** The template needs a content surface for the products built on
it: pages and posts a non-technical author can edit, served as JSON to any
frontend. Should gogogo adopt an existing headless CMS written in Go, and
if so which one?

**Constraint from the ask.** The CMS must be open source, must be in Go, must
support either a SQLite store or GitHub items as the content backend, and
must have seen a commit in the last two months (that is, since 2026-09-11).

**Short answer.** Adopt **none of them as a dependency**. The two Go-native
candidates that survive the filter both fail the stack on a load-bearing
axis (one is GPL-3.0, the other drags a JS toolchain into the build), and
the GitHub-items route is one HTTP call away, not a CMS away. What gogogo
should take is the *shape* of the two strongest candidates — a content-type
model owned by the template, a REST read surface over it, and an admin that
renders through Templ — as a **recipe**, not a dependency.

The GitHub half of the ask was researched separately and is recorded in
full below, because the phrase "issues as pages" is easy to misread. It is
worth the summary here: **GitHub is the authoring surface, the Markdown
file is the content.** An issue becomes a `.md` file with YAML frontmatter
in a directory, and the *generator* maps directories to routes — a job
gogogo does not have, because its routes come from the chi mux. So the
transferable part is the format (Markdown plus frontmatter, which the
vendored docs builder already renders), not the layout.

## Criteria

The gate is the one in the [dependency principles](../dependency-principles.md)
page. Each criterion below is a question a reviewer can answer from the
source, not a preference.

- **Reuse before create** — does the binary already ship it? gogogo already
  owns sessions, RBAC, the DB seam, Templ, and a vendored docs site builder
  that turns Markdown into HTML.
- **Own the seam, rent the depth** — the content model, the authorization
  rule and the wire format are ours; a Markdown parser or a GitHub client
  is rented behind one small interface.
- **Native floor** — one binary, pure Go, no new build tool, no JS at build
  or boot, no new runtime library.
- **Cheapest to remove** — do the candidate's types leak into handlers and
  markup, or stay inside one package?
- **Agent-legible** — can the behaviour be read in one sitting, with Go
  types in and out?
- **License** — MIT or Apache-2.0 keeps the template MIT. GPL and AGPL are
  a distribution decision, not an agent decision.
- **Maintenance signal** — a real release history and more than one
  maintainer, because a template outlives its dependencies.

## The filter applied

Every candidate was probed with the GitHub API on 2026-11-11. `pushed_at` is
the last push to any branch; a release train matters more than a commit, so
the release date is given where one exists.

### Go, and alive in the last two months

| Project | License | Last push | Last release | Store | Verdict |
|---|---|---|---|---|---|
| [olegiv/ocms-go](https://github.com/olegiv/ocms-go) | GPL-3.0 | 2026-11-11 | v0.32.0, 2026-11-10 | SQLite (default), Postgres planned | Closest thing to the ask. Fails on license and on the admin stack. |
| [riducms/ridu](https://github.com/riducms/ridu) | MIT | 2026-11-04 | v0.26.0, 2026-11-03 | SQLite, Postgres, Mongo | Best license and the best fit on paper. Fails on the build toolchain and on bus factor. |
| [lyeve-labs/lyeve-core](https://github.com/lyeve-labs/lyeve-core) | MIT | 2026-11-11 | — | Postgres, MySQL, SQL Server | No SQLite; separate console service. Out of scope. |
| [tx7do/go-wind-cms](https://github.com/tx7do/go-wind-cms) | MIT | 2026-11-10 | — | MySQL, Postgres (ent) | 230 stars, but four frontends and a microservice topology. Wrong shape entirely. |
| [snowztech/barecms](https://github.com/snowztech/barecms) | MIT | 2026-10-01 | — | Postgres only | No SQLite, single maintainer, 12 stars. Out of scope. |
| [togo-framework/cms](https://github.com/togo-framework/cms) | MIT | 2026-10-15 | — | in-memory, pluggable | An in-memory store with a seam that is not yet filled. Not a product. |
| [rokuosan/github-issue-cms](https://github.com/rokuosan/github-issue-cms) | MIT | 2026-11-11 | v1.2.0, 2026-07-26 | GitHub Issues as the authoring surface, Markdown as the artifact | The GitHub-items route, and the only one in Go. 8 stars, Hugo-shaped output, a build step rather than a server. |
| [Martin-Business-Consultants/librepublishgo](https://github.com/Martin-Business-Consultants/librepublishgo) | NOASSERTION | 2026-11-07 | — | SQLite, single static binary | Two months old, zero stars, no license file. Too young to depend on. |
| [Luw17/go-content-go](https://github.com/Luw17/go-content-go) | Apache-2.0 | 2026-11-01 | — | SQLite | Ten days old and 258 KB of repo. Too young to depend on. |
| [wamphlett/blog-server](https://github.com/wamphlett/blog-server) | none | 2026-07-15 | — | Markdown in a cloned repo, indexed in memory | The only Go runtime server of the Git-backed shape, and the closest to what gogogo would need. Unlicensed, unstarred, from 2022. |
| [kostja94/openblog](https://github.com/kostja94/openblog) | MIT | 2026-10-26 | — | Markdown in `content/blog` | Not Go and not a CMS — it is the agent-as-admin inversion. Worth reading, not worth depending on. |

### Filtered out, with the reason

| Project | Reason |
|---|---|
| [pocketbase/pocketbase](https://github.com/pocketbase/pocketbase) | **Archived.** The README says the repo has moved to a personal domain; the last release (v0.40.6, 2026-10-23) is the last one under the org. The most-cited Go answer to "embedded SQLite backend" is no longer maintained where it was. |
| [gouniverse/cms](https://github.com/gouniverse/cms) | Last release 2026-01-20 and the README calls itself outdated, pointing at [dracory/cmsstore](https://github.com/dracory/cmsstore). AGPL-3.0. |
| [emarifer/goCMS](https://github.com/emarifer/goCMS) | Last push 2024-03-26. |
| [restsend/restcontent](https://github.com/restsend/restcontent) | Archived, last push 2024-01-23. |
| [Xzya/headless-cms](https://github.com/Xzya/headless-cms) | Last push 2019-05-05. |
| [skerkour/markdown-ninja](https://github.com/skerkour/markdown-ninja) | Go and actively released, but it is a client for a hosted service: the quickstart needs `MARKDOWN_NINJA_API_KEY` against their API, and the code is AGPL-3.0. The content does not live in your binary or your file. |
| [usememos/memos](https://github.com/usememos/memos) | Not a CMS. A memo app whose API shape happens to be usable. |
| [writefreely/writefreely](https://github.com/writefreely/writefreely) | A blog engine with a fixed posting model, not a content-type API. AGPL-3.0. |
| [siyuan-note/siyuan](https://github.com/siyuan-note/siyuan) | A note-taking app. AGPL-3.0. |

### The Git-based admins, for the GitHub-items half of the ask

Neither is Go, and both are worth knowing because the GitHub-items route
really means "a Git provider is the backend".

| Project | License | Last release | Shape |
|---|---|---|---|
| [sveltia/sveltia-cms](https://github.com/sveltia/sveltia-cms) | MIT | v0.262.4, 2026-11-11 | Git-based, the successor to Decap. 3,073 stars, a release every few days. Editor is a browser app; the content is Markdown in a repo. |
| [decaporg/decap-cms](https://github.com/decaporg/decap-cms) | MIT | active | The incumbent, 19,473 stars. Sveltia exists because Decap stalled. |
| [avitorio/outstatic](https://github.com/avitorio/outstatic) | NOASSERTION | last push 2026-08-27 | 3,169 stars and a hosted dashboard over a GitHub repo, but the license is not OSI and the push is outside the two-month window. |
| [estruyf/vscode-front-matter](https://github.com/estruyf/vscode-front-matter) | MIT | active | 2,552 stars: a CMS that is the editor — VS Code with a content tree. The most editor-native option. |
| [oscarnevarezleal/gh-cms](https://github.com/oscarnevarezleal/gh-cms) | — | last push 2024-06-29 | The GitHub Issues as articles trick, with Hugo. Stale. |

A Git-based admin solves a *static site* problem: an editor commits Markdown
to a branch and a build publishes it. gogogo's product surface is the
opposite — a server-rendered app with an API — so a Git admin would mean
solving a problem gogogo does not have while still needing a real content
model.

The Git-based admins are also where the directory-to-route convention is
documented most explicitly, because each one has to say where the files go:
Sveltia and Decap both take a `folder` per collection plus a `slug`
template (`{{year}}-{{month}}-{{day}}-{{slug}}`), a `media_folder` and a
`public_folder`, and Sveltia adds `root_dir` for a site that lives in a
subdirectory of a monorepo. That configuration surface is the shape of a
content model expressed as a directory layout — the useful thing to learn
from it is the *field schema*, not the layout.

## Why the two finalists were not adopted

### ocms-go: the right shape, the wrong license and the wrong admin

ocms-go is the closest candidate in the ecosystem, and it is close for
structural reasons: a single binary, SQLite by default, `chi` and `a-h/templ`
(the same two the template already uses), a `huma` REST surface, an
embedded admin, and a real feature set — content types, scheduled
publishing, media library, FTS5 search, multi-language, webhooks. It
declares a v1.0 milestone and ships weekly releases (v0.30.0 on 2026-10-23,
v0.31.0 on 2026-11-04, v0.32.0 on 2026-11-10).

Two things disqualify it.

**The license is GPL-3.0.** gogogo is MIT and ships as a template whose
users redistribute it. A GPL dependency linked into the binary is a
distribution decision for the whole project, and the AGPL/GPL obligations
of the surrounding product are not an agent's to take. This is a hard
block, not a preference.

**The admin is HTMX + Alpine.** gogogo's stack rule is explicit: no HTMX,
no Alpine, one reactive vocabulary (Datastar over SSE). ocms-go's admin
would be a second vocabulary in the same product, and its Dockerfile adds
`nodejs`/`npm` and a Sass plus Tailwind asset pipeline to build it —
gogogo builds CSS once with the Tailwind CLI and embeds it, and the binary
carries no JS toolchain. Adopting ocms-go would raise the native floor on
the build side, which is the one axis the principles call a ban.

There is also a bus-factor note: 1,625 of its commits are from one author.

### ridu: the best license, the best fit, the wrong shape

ridu is the most interesting candidate and the one that took the longest to
rule out. It is MIT. It is config-as-code in Go — content types, fields,
access control, hooks and plugins declared in Go, and that executable
configuration becomes the schema manifest, the REST API, the generated
TypeScript contracts and the authoring interface at once. It has an embedded
admin in the same binary, an MCP server for agent access, field-level access
control, drafts and versions. It uses `modernc.org/sqlite`, so it is pure
Go, no CGO. It is young (created 2026-09-03) but it released v0.24.0,
v0.25.0 and v0.26.0 in October and November.

What rules it out for gogogo is the shape, not the quality.

**It is a backend framework, not a CMS.** Its own README positions it
against Payload and PocketBase as "a config-as-code backend framework for
Go". Adopting it means adopting its auth, its sessions, its access-control
model, its admin UI, its plugin system and its route mounting. That is
replacing gogogo's stack, not adding a content surface to it — the exact
inverse of the "own the seam, rent the depth" decision the template has
already made everywhere else. The template owns its auth precisely so that
it can be owned; a framework that brings its own is a rewrite, and the
principles call "an exit that is a rewrite" a human gate.

**The admin is Svelte 5 and the build needs Bun.** `bun run
generate:admin-assets` is in its own Makefile for the runtime packages. The
JS toolchain at build time is the native-floor ban again, in a second
flavour. It also means a `node_modules` tree and a second template
vocabulary beside Templ and DaisyUI.

**Bus factor is one.** One author, 44 commits, and the README warns that
"compatibility may change between minor releases" while pre-1.0. A template
that pins dependencies cannot pin a framework that breaks its own API
between minor versions.

**Its database story is three engines.** SQLite, Postgres and MongoDB. The
template has one engine and one schema
([stack layers](../stack-layers.md)) and a deliberate upgrade ladder
([stack tradeoffs](../stack-tradeoffs.md)). Adopting a CMS with three
adapters is the opposite of a flat stack.

Worth keeping: ridu's *config-as-code* idea is the strongest single idea in
the survey, and it is already the template's own approach to the capability
registry. Its draft/version model and its field-level access control are
both things the content surface wants.

### The GitHub-items route

The GitHub-items half of the ask has exactly one Go implementation with
real traction — `github-issue-cms`, MIT, active, using `go-github` to read
Issues and write Hugo-shaped Markdown. It is small (8 stars), its output is
tied to Hugo's content layout, and it is a one-shot generator, not a
serving CMS.

#### What "issues as pages" actually means

The phrase is the part worth being precise about, because it is easy to
read as "the issue *is* the published page" and it is not. Reading the
implementation settles it: **GitHub is the authoring surface, the Markdown
file is the content.** The issue is where an author types, with comments,
labels, permissions and review; the published artifact is a file on disk.

The pipeline in `github-issue-cms` is four small pieces, and reading them
makes the whole pattern legible:

| Piece | What it does |
|---|---|
| `pkg/core/github_issues.go` | `ListIssues` over the REST API, `State: "all"`, pull requests filtered out |
| `pkg/core/article_parser.go` | parses YAML frontmatter out of the issue body |
| `pkg/core/article_renderer.go` | renders the entry as `---\n{yaml}---\n\n{body}\n` |
| `pkg/core/filesystem_articles.go` | writes it to `content/posts/<date>.md`, resolving a filename template like `%Y-%m-%d_%H%M%S.md` |

So an issue becomes a Markdown file with YAML frontmatter, in a directory.
The route is not the issue's — it is whatever the site generator makes of
the directory.

**Directory-to-route is the convention, and it is the generator's job.**
Astro states it most explicitly: content collections live outside
`src/pages/`, so no route is generated by default; a dynamic route maps the
request parameter to the entry, and the entry's `id` is its path relative to
the collection folder — `src/blog/hello-world.md` becomes id `hello-world`
and therefore `/posts/hello-world/`. A `slug` in the frontmatter overrides
that id, and may contain `/` (which then needs a rest parameter). Hugo
follows the same shape with `content/posts/x.md` → `/posts/x/`. Sveltia and
Decap expose it as configuration: a `folder` per collection plus a `slug`
template, with `{{dirname}}` and `{{slug}}` available in media paths.

This is worth stating plainly because it settles the question the ask was
really asking: **yes, the models that use GitHub as a CMS save Markdown and
use directories for routes** — but the CMS only writes files. It is the
static generator or the server that turns a directory into URLs.

#### Four models, not one

"GitHub as CMS" covers four different shapes in 2026, and conflating them
is what makes the question feel unanswerable:

| Model | The author writes in | The content lives in | Example |
|---|---|---|---|
| Git-based CMS | an admin SPA that writes through the provider's API | Markdown + frontmatter in the repo | Sveltia, Decap, OutStatic, Front Matter |
| Issues as the authoring surface | a GitHub issue | a generated `.md` file | `github-issue-cms` (Go), `gh-cms` |
| Live API | a GitHub issue or discussion | nowhere — fetched at build or request time | Astro content loaders, `github-discussions-blog-loader` |
| Git-backed runtime server | the repo | Markdown, cloned and indexed in memory | `blog-server` (Go) |

The last row is the one closest to gogogo's shape, and it is the weakest:
`blog-server` serves `/topics/{topic}/articles/{article}` over JSON with an
in-memory index and a git clone on boot, but it has no license, no stars,
and was created in 2022. The second row is the only Go implementation with
momentum, and it is a build step rather than a server.

Nothing in Go serves GitHub Issues as a live API at runtime — the search for
a Go server doing that returns nothing. That is not an accident: it is the
same conclusion the rest of this note reaches from the other direction.

#### The one that is actually new

The most interesting 2026 artifact is
[openblog](https://github.com/kostja94/openblog) (MIT, created 2026-08-29),
whose pitch is *"Git is the CMS. Your coding agent is the admin."* It keeps
Markdown in `content/blog`, mounts at `/blog` inside an existing product
site, ships taxonomy, RSS, sitemap and JSON-LD without a CMS database, and
routes editorial work through pull requests — with agent skills for the
maintenance workflow.

It is Next.js, not Go, and at 7 stars it is unproven. But it inverts the
usual arrangement in a way gogogo would recognise: there is no admin UI at
all, because the reviewing agent is the editor, and Git's pull-request flow
is the editorial workflow. That is the same instinct as gogogo's own
recipe model — code is copied and owned, schema is declared — applied to
content.

#### What it means for the template

**The GitHub-items idea does not need a CMS at all.** It needs a client,
and gogogo already talks to the GitHub API twice: the OAuth preset in
`internal/auth/oauth.go` and the release check in
`internal/installer/update.go`. A content source that reads Issues or
Discussions over the API is a seam over one HTTP call, not a platform. The
"admin" for that source is GitHub's own issue form, which already exists and
already has the right permissions.

One caveat worth recording: gogogo does **not** ship `go-github`. Both
existing GitHub call sites use `net/http` plus `encoding/json` directly
(`internal/installer/update.go` and `internal/auth/oauth.go`). Adding a
GitHub-items source therefore means either adopting `go-github` as a new
dependency — which the dependency principles make a written trade-off, not
a default — or following the existing pattern of a small hand-rolled
client. The existing pattern is the cheaper one, and it is the one this
template already uses twice.

## The recommendation: a content recipe, not a CMS dependency

The template's own answer to "a feature a user wants" is already settled and
documented: a **recipe** — a slice copied into the user's repository and
owned from then on, carrying a receipt, with its schema *declared* and
applied by the core engine ([extensibility](extensibility-2026-11.md)). The
pomodoro recipe is the shape to copy: one table, one migration, one query
file, one page, one write endpoint, one sidebar entry.

A content slice fits that shape exactly, and the pieces it needs are the
ones the template already owns:

| The slice needs | gogogo already has |
|---|---|
| storage with owned queries | `internal/db` + sqlc + the migration ledger in `internal/dbschema` |
| a typed content model | Go structs in the slice, like `features/todo/models.go` |
| an author-facing page | Templ + Datastar, the one reactive vocabulary |
| authorization | Casbin, route plus verb policy, one owner per row |
| read API for other clients | the `POST /api/ext/invoke` route and the recipe `invoke` path |
| Markdown rendering | the vendored `site/build.mjs` renderer, already turning Markdown into HTML |
| search | `internal/search` (BM25 plus fuzzy), already shipped |

Two storage backends are worth supporting, and they are not symmetric:

1. **SQLite (the default).** The slice owns a table; the content lives
   beside everything else, in the same Turso file, with the same backup
   story.
2. **GitHub items (an optional source).** A read seam that pulls Issues or
   Discussions into the same content model over the GitHub API. The
   existing OAuth preset is the auth; the existing hand-rolled HTTP pattern
   is the transport.

Both are "the content model plus one seam", which is the shape the template
already prefers over "a platform".

#### Why the directory-to-route convention does not transfer

The 2026 Git-based models all map a directory to a route, and it is tempting
to read that as a rule gogogo should copy: content lives in `content/posts/`
and the route follows. It does not transfer, for one concrete reason: **that
mapping is the generator's job, and gogogo has no generator.** Its routes
come from the chi mux, one `MountFeature` per capability, and its content —
if a slice owns it — comes from a table.

Copying the convention would re-introduce the thing the template removed: a
second place where a route is decided, derived from a filesystem layout,
that must be kept in sync with the mux. The template's own rule is that a
route is declared, not inferred — the recipe contract declares its routes,
and the receipt checks them against what the handler registers.

What does transfer is the *format*, not the *layout*: **a Markdown file
with YAML frontmatter is a sufficient content format**, and the template
already has a renderer for it. The vendored `site/build.mjs` turns Markdown
into HTML today — headings with GitHub-compatible anchor ids, tables,
fenced code, blockquotes and lists — so a content slice can render the same
body without a new dependency.

It does not parse frontmatter, and it does not need to: `site/build.mjs`
takes its title and description from the MANIFEST and derives the slug from
the filename. A content slice that wants frontmatter would parse it once,
at import, into typed columns — the same move every model in the survey
makes when it writes a file, except gogogo would write a row instead of a
file. That keeps the template's own rule: the stored form is typed, the
authoring form is prose.

### What to take from each candidate

| From | Take | Do not take |
|---|---|---|
| ocms-go | The feature list as a checklist: content types, scheduled publishing, media library, FTS5 search, multi-language, webhooks. | The license, the HTMX + Alpine admin, the Sass/Tailwind asset pipeline. |
| ridu | Config-as-code content types, field-level access control, drafts and versions, the embedded-admin idea. | The framework, the Svelte admin, the Bun build step, the three database adapters. |
| sveltia / Decap | The proof that a Markdown-plus-frontmatter file is a sufficient content format, and that a git backend needs no server. | The static-site shape, and the directory-to-route convention — that is the generator's job, and gogogo has no generator. |
| github-issue-cms | The proof that Issues are a workable authoring surface, and the four-piece pipeline that turns one into a file: list, parse frontmatter, render, write. | The Hugo-specific output layout, and the assumption that the artifact must be a file rather than a row. |
| openblog | The inversion worth trying: no admin UI, because the reviewing agent is the editor and a pull request is the editorial workflow. | The Next.js stack, and the premise that content belongs in git rather than in the app's own store. |
| PocketBase | The reminder that "embedded SQLite backend" is a category with one famous vendor, and that vendor archived the repo. | Anything. |

## Revisit trigger

Revisit this page when one of these is true:

- A Go headless CMS releases under MIT **and** ships a Templ-based or
  headless-admin-free admin **and** has more than one maintainer.
  That combination does not exist today.
- gogogo ships a content surface of its own (the recipe above lands), and
  the question becomes "what does a *second* content source need" — at
  which point the GitHub-items seam is the first candidate.
- A user asks for a CMS with a capability the recipe does not have
  (scheduled publishing, media transcoding, multi-language routing) — each
  one is a trigger to look again rather than to build it in.
- A Git-backed runtime server in Go appears with a real license and
  traction. `blog-server` is the closest thing today (Markdown in a cloned
  repo, indexed in memory, served as JSON) and it is unlicensed, unstarred
  and from 2022 — a server that clones a repo and serves it is a small
  thing, and its absence is a gap, not a proof of difficulty.

## Related

- [Dependency principles](../dependency-principles.md)
- [Stack layers](../stack-layers.md)
- [Stack tradeoffs](../stack-tradeoffs.md)
- [Extensibility](extensibility-2026-11.md)
- [Recipe metrics and panel spec](recipe-metrics-panel-2026-11.md)
- [Todo example](../todo-example.md)
---
name: gogogo-ui
description: "UI standards for the gogogo template — DaisyUI v5 components, Templ markup, Datastar signals, themes. Triggers when: editing .templ files, src/css/input.css, internal/ui shell, features/*/components, the theme catalog in internal/themes, or styling, designing or laying out any page. Enforces one vocabulary, semantic colors, compiled app.min.css. Read before any UI or CSS change."
---

# gogogo-ui

Template UI rules. The component vocabulary itself lives in the vendored
`daisyui` skill — this wrapper states what the template holds itself to on top.

## MUST read before any .templ or CSS edit

Read in this order; each one answers a different question:

1. `../daisyui/SKILL.md` — the component vocabulary (class names, variants,
   themes). Vendored byte-identical from `https://daisyui.com/SKILL.md`,
   pinned in `../daisyui/UPSTREAM_SHA`. Never edit it in place — a local edit
   makes the pin a lie. `bin/check-daisyui-drift.sh` (advisory in pre-commit)
   fails when the copy diverges from the pin or upstream has moved. To UPDATE
   it, run `scripts/refresh-daisyui.sh` — never copy by hand.
2. `../../docs/ui-themes.md` — the DaisyUI + themes guide, the theme list, the
   `make css` / `make css-check` flow. Normative for this repo.
3. `../gogogo-coding-standards/references/datastar.md` — the Datastar gate
   (`.templ` attributes, Go SDK calls, signal ownership). UI markup that
   breaks it fails `make datastar-lint`.
4. `../../internal/themes/` — the shipped theme catalog (`themes.go`;
   `themes_test.go` keeps it and `src/css/input.css` in step). The truth
   about which palettes exist and which are dark.

A skill's `references/` resolve only as relative paths to a locally present
skill, so read the local vendored copy — a URL link cannot reach them.

## Translate, don't transplant (daisyui examples are HTML/JSX, this repo is .templ)

The vendored skill's framing and install section do not apply here. The
full translation layer lives in `references/daisyui-templ.md` — read it
with the vendored skill: it states the `class=`-never-`className` rule,
the `.templ` expression mapping, and the house exemplar, plus the currency
contract (upstream component churn never touches it).

## Rules

- One vocabulary: DaisyUI. Markup uses DaisyUI components plus Tailwind
  utilities, rendered through `internal/components.Assets()`. It MUST NOT
  fork a page — no duplicated markup per variant.
- Semantic colors only: `bg-base-*`, `text-base-content`, `bg-primary`,
  `text-error` and their peers. Never `dark:` with a hard-coded color —
  the theme owns the palette, the markup only names the role.
- One bundle: `/static/app.min.css`. Never a second stylesheet, never a
  `daisyui.min.css` relic, never CDN CSS in markup. After a class change run
  `make templ && make css` so `make css-check` stays green.
- Scan roots are `features/`, `web/`, `internal/` only (`src/css/input.css`).
  `site/` and `docs/` edits cannot stale the bundle — and must not be
  scanned to make them do so.
- Markup follows the vendored DaisyUI vocabulary. Deterministic backstop:
  `make check-no-jsx` fails on `className` in `.templ` (the one JSX-ism
  templ passes through silently — verified: compiles clean, renders
  verbatim, unstyled) and on CDN links in app markup. An invented component
  renders unstyled: Tailwind emits only what it scans, and nothing in this
  tree contains the classes a model invents at runtime (the `@source inline`
  safelist in `src/css/input.css` is the bounded exception, kept in step
  with `features/genui/components/htmlclasses.go`).

## When to Use

Activate when: editing any `.templ` file, `src/css/input.css`,
`web/` templates or skins, the `internal/ui` shell, any
`features/*/components/` directory, the theme catalog in
`internal/themes`, or styling any page.

A `.templ` edit fires two skills on purpose: `gogogo-coding-standards` owns
the Datastar/backend gate (`PatchElements`, signal ownership), this one owns
the vocabulary and the CSS. Read both before writing markup.

Do NOT activate for: Go-only changes (that is `gogogo-coding-standards`
alone), landing-page copy, release notes, or `site/` stylesheet work
(separate bundle).

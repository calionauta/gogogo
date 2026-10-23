# daisyui → .templ translation

The vendored skill (`../daisyui/SKILL.md`) frames itself as "HTML or JSX" and
carries install instructions (`npm i -D daisyui@latest`, CDN links,
`tailwind.config.js` notes) plus one React-only part (DayPicker). None of
that applies here. This file is the complete translation layer: everything
in it is template-owned and changes only when OUR idioms change (a Templ
or Datastar version bump, a new house exemplar) — never when upstream adds
a component, because class names are framework-agnostic strings and the
rules below are generic over all ~70 catalogued components.

## Currency contract

- Upstream churn (new components, renamed classes, new themes) lands ONLY
  in the byte-identical vendored copy via `scripts/refresh-daisyui.sh`.
  This file does not name a single component, so it cannot go stale that way.
- This file changes ONLY when: Templ expression syntax changes, Datastar
  attribute rules change (`../gogogo-coding-standards/references/datastar.md`
  is normative there), or the house exemplar moves. Each change is reviewed
  like any template rule — it is not a mechanical refresh.

## Rules

- Take ONLY the vocabulary: component class names, variants, and structure.
  Leave the install section alone — the dependency (`daisyui` in
  `package.json`), the `@plugin "daisyui"` line and the theme list already
  exist in `src/css/input.css`. Never `npm i`, never a CDN link, never a
  `tailwind.config.js` (deprecated in v4).
- Write `class=`, never `className`. (`make check-no-jsx` fails the build
  on `className` in `.templ`: templ passes unknown attributes through
  verbatim, so it compiles clean and renders unstyled — the one JSX-ism
  that fails silently. Everything else JSX fails loudly in `templ generate`.)
- JSX braces and `map()` become `.templ` `if`/`for` blocks with `{ expr }`
  and conditional `?={ }` attrs; React-only components have no equivalent —
  rebuild them from DaisyUI primitives.
- House shape to imitate: `features/todo/components/todo_item.templ`
  (`class=`, `checked?={ }`, `if`/`else`, `data-on:*` on DaisyUI classes).

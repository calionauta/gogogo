# UI sounds

Every interactive action ships with audio feedback out of the box — a curated
sound palette ([cuelume](https://github.com/Danilaa1/cuelume), MIT),
**vendored into the repo rather than installed as a dependency**. No
`package.json` entry, no `go.mod` entry, no network fetch at build or runtime.
Sounds are synthesized live with the Web Audio API — there are no audio files.

## It's a plugin, not baked into the app

The plugin (`features/sounds/`) owns its whole surface:

- the script loader (`@sounds.SoundAssets()` in each page's `<head>`),
- the navbar mute toggle (`@sounds.SoundToggle()`),
- the client glue (`web/resources/static/cuelume.js`).

The glue ships the sound accessibility contract out of the box.

## The accessibility contract

1. **`prefers-reduced-motion` respected by default.** OS-level reduced motion
   auto-mutes playback and reacts to runtime preference changes. There is no
   in-app override — it's an accessibility setting, not a preference.
2. **Global mute with a real control.** The navbar 🔊 button toggles sound
   on/off, persists the choice in `localStorage` (key `gogogo_sound`), is
   keyboard-accessible (`aria-pressed` on a real `<button>`), and survives
   Datastar DOM morphs (delegated click, same pattern as `theme.js`). The
   button always reflects and responds to the user's own choice — if
   reduced-motion keeps playback muted anyway, the button still flips and its
   tooltip explains the system override, so it never looks dead.
3. **Subtle default volume.** cuelume's mixer runs hotter than libraries tuned
   for a 30% default; `DEFAULT_VOLUME` (`0.4`) keeps every cue audible without
   being intrusive. Tune it in `cuelume.js`.
4. **Sounds are additive only.** Every cue pairs with existing visual feedback
   (toasts, button states, spinners) and never replaces it.

## Behavior wiring

**`bind()`** enables declarative `data-cuelume-*` attributes
(`data-cuelume-press`, `-release`, `-hover`, `-toggle`) anywhere, so
per-element sounds work without touching the glue.

**Global press sound** — a delegated `pointerdown` listener plays the `press`
knock on every button, `role="button"`/`role="tab"`, `.btn` link, and checkbox.
It works on mouse, touch, and pen (pointer events) and covers
Datastar-morphed DOM.

**Toast-type chimes** — a `MutationObserver` on `#toast-container` plays a cue
matching the toast type:

| Toast class | Cue |
|---|---|
| `alert-success` | `success` |
| `alert-error` | `error` |
| `alert-warning` | `loading` — cuelume has no dedicated warning cue, so the rising shimmer reads as "still working" |
| `alert-info` | `page` |

No server changes needed: the existing SSE toast path (create, delete, clear,
workflow completion, retry/Suggest failures) feeds the sounds.

**`window.Cuelume`** is a tiny public API (`play`, `setEnabled`, `setVolume`,
`isEnabled`) for future settings surfaces.

## Customizing

Since cuelume is client-side only, your app owns the settings:

```js
import { play, setVolume } from "/static/cuelume/index.js";

play("sparkle");   // play any of the 14 sounds imperatively
setVolume(0.6);    // global volume, clamped to 0–1 (default 0.4)
```

To add a per-element sound, drop a `data-cuelume-*` attribute on the element —
`bind()` picks it up automatically, including elements added by Datastar later.

## Update and remove

**Update:** re-download cuelume's `dist/` into
`web/resources/static/cuelume/` (keep the `LICENSE`).

**Remove:** delete `features/sounds/`, drop `@sounds.SoundAssets()` from the
page layouts and `@sounds.SoundToggle()` from the navbar, then delete
`web/resources/static/cuelume.js` and `web/resources/static/cuelume/`. The full
checklist lives in the `SCOPE:layer=feature,removal=plugin` doc comment in
`features/sounds/sounds.go`.

## Related

- [UI skins](ui-skins.md) — the other pluggable UI surface.
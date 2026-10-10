// SCOPE:layer=feature,removal=plugin — UI sound feedback plugin (cuelume).
//
// Adds interaction sounds to the app through the vendored cuelume library
// (web/resources/static/cuelume/, MIT — not a package dependency). This
// package is the plugin's Go surface: it renders the script loader and the
// navbar mute toggle. All behavior lives in the self-contained glue module
// web/resources/static/cuelume.js, which ships the best practices out of
// the box:
//
//   - prefers-reduced-motion is respected by default — OS-level reduced
//     motion auto-mutes and reacts to runtime preference changes.
//   - Global mute: a persistent toggle in the navbar (stored in
//     localStorage under "gogogo_sound"), keyboard-accessible via
//     aria-pressed on the button.
//   - Subtle default volume (cuelume's mixer runs hot; 0.4 keeps the cues
//     audible without being intrusive — tune DEFAULT_VOLUME in cuelume.js).
//   - Sounds are additive only: every cue pairs with existing visual
//     feedback (toasts, button states) and never replaces it.
//
// Sonic vocabulary (one mapping for every feature — do not invent
// per-page cues; the glue below fires most of these with zero markup):
//
//   - Any press on any interactive element → "press" (delegated global
//     listener, capture phase: covers Datastar-morphed DOM with no
//     per-element wiring).
//   - Toast appears → cue by toast type: success → "success",
//     error → "error", warning → "loading", info → "page"
//     (MutationObserver on #toast-container).
//   - Navbar hover → "tick", navbar press → "toggle" (explicit
//     data-cuelume-* attrs, the only per-element wiring in the app).
//   - Destructive press (todo delete) → "droplet" (explicit override).
//   - Sound re-enabled → "chime" (confirmation the system is live;
//     muting plays nothing on purpose).
//
// A new feature gets full sound behavior by rendering the shared toast
// container + navbar: no per-feature JS, no per-button attrs, unless it
// needs an override from the table above.
//
// REMOVE (plugin): delete this package, drop @sounds.SoundAssets() from
// the page layouts (features/todo/components/layout.templ, landing,
// config, auth LoginPage, whiteboard) and @sounds.SoundToggle() from the
// navbar (features/auth/views.templ), then delete
// web/resources/static/cuelume.js + web/resources/static/cuelume/.
package sounds

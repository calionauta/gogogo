// Cuelume sound integration — VENDORED, plugin-style (features/sounds).
//
// Cuelume (https://github.com/Danilaa1/cuelume, MIT) is a curated palette
// of interaction sounds synthesized live with the Web Audio API — no audio
// files, no runtime dependencies. The library ships inside this repo under
// /static/cuelume/ (MIT license file alongside), so the binary has zero
// external deps. This module is the plugin's glue and owns the sound
// accessibility contract:
//
//   1. prefers-reduced-motion is respected by default — OS-level reduced
//      motion auto-mutes playback and reacts to runtime preference changes.
//      Opt out by clearing your OS setting; there is no in-app override
//      because this is an accessibility preference, not a taste setting.
//   2. Global mute — a [data-sound-toggle] control persists a preference in
//      localStorage ("gogogo_sound", "on"|"off"): the account panel's switch
//      (a checkbox's `checked`). It is keyboard-accessible and survives
//      Datastar morphs (delegated change, same pattern as theme.js).
//   3. Subtle default volume — cuelume's mixer runs hotter than libraries
//      tuned for a 30% default; 0.4 keeps every cue audible without being
//      intrusive. Tune DEFAULT_VOLUME here (the API below also exposes
//      setVolume for a future settings panel).
//   4. Sounds are additive — every cue pairs with existing visual feedback
//      (toasts, button states, spinners) and never replaces it.
//
// Behavior wiring:
//   - bind()                     — enables declarative data-cuelume-* attrs.
//   - Global press               — pointer press on any interactive element
//                                 plays "press". Delegated (capture phase)
//                                 so Datastar-morphed DOM is covered;
//                                 elements with data-cuelume-press are left
//                                 to bind() to avoid double-firing. Works
//                                 on mouse, touch, and pen (pointer events).
//   - Toast feedback             — a MutationObserver on #toast-container
//                                 plays "success"/"error" when matching
//                                 toasts appear (visual equivalent always
//                                 present, so sound stays additive).
//
// Removing the plugin: delete features/sounds/, this file, the
// /static/cuelume/ directory, and the @sounds.SoundAssets() /
// @sounds.SoundSwitch() calls from the templates.
import { bind, play, setEnabled, setVolume } from "./cuelume/index.js";

var STORAGE_KEY = "gogogo_sound";
var DEFAULT_VOLUME = 0.4;
// Played when the user RE-ENABLES sound from the sound switch: a distinct,
// pleasant confirmation that the sound system is live. Muting plays nothing
// on purpose. Tune the cue here (cuelume's "chime" is a soft ascending bell).
var ENABLE_SOUND = "chime";
// Toast-type sounds. Kept as named constants so every action of a given type
// (button, toast, or anything else) reuses the SAME cue — a consistent sonic
// vocabulary: success → success, failure → error, warning → loading (a brief
// unresolved rising shimmer, cuelume has no dedicated "warning" cue), and
// info → page (a neutral papery flick). Tune per type here.
var SUCCESS_SOUND = "success";
var ERROR_SOUND = "error";
var WARNING_SOUND = "loading";
var INFO_SOUND = "page";

bind();
setVolume(DEFAULT_VOLUME);

// --- preference state -----------------------------------------------------
// Two independent inputs, combined with logical AND:
//   userPref      — explicit sound switch, persisted in localStorage.
//   reducedMotion — OS-level prefers-reduced-motion (auto-mute).
var state = {
  userPref: readPref(),
  reducedMotion: false,
};

function readPref() {
  try {
    return localStorage.getItem(STORAGE_KEY) === "off" ? "off" : "on";
  } catch (e) {
    return "on";
  }
}

function effective() {
  return state.userPref === "on" && !state.reducedMotion;
}

function apply() {
  setEnabled(effective());
  syncToggleUI();
}

// Keep the switch in sync with the stored preference. It reflects the USER
// preference, so it responds even when prefers-reduced-motion keeps playback
// muted. The title explains the override so a muted-but-on state is never
// silent confusion.
function syncToggleUI() {
  var els = document.querySelectorAll("[data-sound-toggle]");
  if (!els.length) return;
  var prefOn = state.userPref === "on";
  var title = state.reducedMotion && prefOn
    ? "Sound on — muted by your device's reduced-motion setting"
    : prefOn ? "Mute sounds" : "Unmute sounds";
  // The native checked state IS the preference. Safe because the delegated
  // change handler owns writes, so the browser never drifts from this value.
  els.forEach(function (el) {
    el.checked = prefOn;
    el.setAttribute("title", title);
  });
}

function setPref(on) {
  state.userPref = on ? "on" : "off";
  try {
    localStorage.setItem(STORAGE_KEY, state.userPref);
  } catch (e) {
    /* private mode / quota — preference applies for this session */
  }
  apply();
  // Re-enabling sound gets a distinct, pleasant confirmation that playback
  // is live again; muting is deliberately silent (turning sound OFF should
  // make no noise). Only play when sound is truly audible — reduced-motion
  // keeps it muted even if the user flips the preference to on.
  if (effective()) {
    play(ENABLE_SOUND);
  }
}

// Resolve a selector along the composed event path. A pointer event fired on
// an <svg> inside a button has a target whose .closest() still resolves, but
// walking composedPath() covers every nesting (including any shadow root a
// future icon might render in), so delegated handlers never miss the host.
function closestInComposedPath(e, sel) {
  var path = e.composedPath ? e.composedPath() : e.path || [e.target];
  for (var i = 0; i < path.length; i++) {
    var n = path[i];
    if (n && n.nodeType === 1 && n.closest) {
      var hit = n.closest(sel);
      if (hit) return hit;
    }
  }
  return null;
}

// Delegated wiring, so it survives Datastar morphs and re-renders (same
// pattern as theme.js). The switch IS a checkbox: the browser has already
// flipped it by the time the event bubbles, so read the new state on `change`
// — writing `checked` from a click handler races the activation behaviour.
document.addEventListener("change", function (e) {
  var t = closestInComposedPath(e, "input[data-sound-toggle]");
  if (!t) return;
  setPref(t.checked);
});

// OS-level reduced-motion: mute by default and react to live changes.
var mq = window.matchMedia("(prefers-reduced-motion: reduce)");
state.reducedMotion = mq.matches;
function onMqChange(e) {
  state.reducedMotion = e.matches;
  apply();
}
if (mq.addEventListener) mq.addEventListener("change", onMqChange);
else if (mq.addListener) mq.addListener(onMqChange); // Safari < 14

apply();

// --- global button-press feedback -----------------------------------------
// A muted knock on any pointer press over a button-like element. The
// selector mirrors cuelume's own defaults so "each action has a sound"
// holds without tagging every button. play() no-ops when muted.
function pressFromEvent(e) {
  var t = closestInComposedPath(
    e,
    'button, [role="button"], [role="tab"], [class*="btn"], input[type="checkbox"], input[type="submit"]'
  );
  if (!t) return;
  // The sound toggle is exempt from the generic press: muting must be
  // silent, and enabling is confirmed by its own dedicated cue (ENABLE_SOUND).
  if (t.hasAttribute("data-sound-toggle")) return;
  if (t.hasAttribute("data-cuelume-press")) return;
  play("press");
}
document.addEventListener("pointerdown", pressFromEvent, true);

// --- success / error feedback via toasts ----------------------------------
// Every backend toast lands in #toast-container as a .toast-msg whose
// inner alert carries the type class (alert-success / alert-error).
function toastAdded(mutations) {
  for (var i = 0; i < mutations.length; i++) {
    var added = mutations[i].addedNodes;
    for (var j = 0; j < added.length; j++) {
      var node = added[j];
      if (!node || node.nodeType !== 1) continue;
      var toast =
        node.classList && node.classList.contains("toast-msg")
          ? node
          : node.querySelector && node.querySelector(".toast-msg");
      if (!toast) continue;
      // Every toast type has a cue: success / error / warning / info.
      if (toast.querySelector(".alert-success")) play(SUCCESS_SOUND);
      else if (toast.querySelector(".alert-error")) play(ERROR_SOUND);
      else if (toast.querySelector(".alert-warning")) play(WARNING_SOUND);
      else play(INFO_SOUND); // alert-info (and any default toast)
    }
  }
}
function watchToasts() {
  var container = document.getElementById("toast-container");
  if (!container) return;
  new MutationObserver(toastAdded).observe(container, { childList: true });
}
if (document.readyState === "loading") {
  document.addEventListener("DOMContentLoaded", watchToasts);
} else {
  watchToasts();
}

// --- tiny public API for future settings surfaces --------------------------
window.Cuelume = {
  play: play,
  setEnabled: setEnabled,
  setVolume: setVolume,
  isEnabled: effective,
};

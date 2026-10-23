// Theme controller — dark/light mode with localStorage persistence.
//
// Design notes (kept deliberately small and dependency-free):
//   - The $theme SIGNAL owns the `data-theme` attribute on <html> (via the
//     `data-attr:data-theme` binding every page ships) and the `dark` class
//     (via `data-class:dark`). Nothing in this module writes either one:
//     Datastar watches the DOM and reverts external writes to bound
//     attributes, so an imperative setAttribute here would be clobbered
//     back to the signal value (a toggle that changes nothing and hides
//     both icons).
//   - The navbar button flips the signal with
//     `data-on:click="$theme = ...; localStorage.setItem('themeMode', $theme)"`.
//     The toggle icons are inline SVG (internal/icon.Icon) whose visibility
//     the [data-theme] CSS rules in app.css own outright — no script touches
//     them, so there is no icon state to sync. This module owns the
//     pre-paint bootstrap read, the OS-preference fallback, and the isDark
//     helper the data-class:dark binding calls.
//   - Storage key is "themeMode"; value is "light" | "dark". Falls back to
//     the OS preference (prefers-color-scheme) when nothing is stored, and
//     to "light" if even that is unavailable (older browsers/private mode).
//   - Live OS-preference changes are intentionally NOT followed: with no
//     stored choice the pre-paint bootstrap already honored the OS once,
//     and a mid-session flip would need signal access this module does not
//     (and must not) have. Toggling writes the choice, which wins forever.
(function () {
  "use strict";

  var DARK = "dark";
  var LIGHT = "light";

  // Which themes are dark comes from the document: authviews.ThemeRootAttrs
  // emits data-theme-dark from internal/themes, so this module holds no second
  // copy of the catalog.
  function darkNames() {
    return (document.documentElement.getAttribute("data-theme-dark") || "").split(" ");
  }

  function isDark(theme) {
    if (darkNames().indexOf(theme) >= 0) return true;
    if (theme === LIGHT) return false;
    return prefersDark();
  }

  function current() {
    var t = document.documentElement.getAttribute("data-theme");
    if (t === DARK || t === LIGHT) return t;
    return prefersDark() ? DARK : LIGHT;
  }

  function prefersDark() {
    return (
      window.matchMedia &&
      window.matchMedia("(prefers-color-scheme: dark)").matches
    );
  }

  // Minimal stable API: read the current theme, test darkness. Writes go
  // through the $theme signal (see the button); nothing here touches
  // data-theme or the dark class.
  window.Theme = {
    get: current,
    current: current,
    isDark: isDark,
  };
})();

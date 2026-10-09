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
//     `data-on:click="$theme = ...; localStorage.setItem('themeMode', $theme);
//     Theme.syncIcons($theme)"`. This module owns icon sync (the iconify
//     quirk), the pre-paint bootstrap read, and the OS-preference fallback.
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

  // Explicitly show only the active theme's icon. In light mode the sun
  // (icon-light-mode) is shown; in dark mode the moon is shown. The CSS
  // [data-theme] rules are the primary mechanism, but iconify-icon is a
  // custom element whose own stylesheet can override the host display;
  // setting it directly here guarantees exactly one icon is visible
  // regardless of cascade order. Called on init and from the button's
  // data-on:click after the signal flips (inline styles on the icons are
  // NOT data-bound, so nothing reverts them).
  function syncIcons(theme) {
    var icons = document.querySelectorAll(".theme-toggle-icon");
    icons.forEach(function (el) {
      var isDarkIcon = el.classList.contains("icon-dark-mode");
      // In light mode show the sun (light icon); in dark mode show the moon.
      var active = (theme === LIGHT) !== isDarkIcon;
      el.style.display = active ? "" : "none";
    });
  }

  // Minimal stable API: read the current theme, fix the icons. Writes go
  // through the $theme signal (see the button); nothing here touches
  // data-theme or the dark class.
  window.Theme = {
    get: current,
    current: current,
    syncIcons: syncIcons,
  };

  // Initial icon sync: ensure exactly one toggle icon is visible based on
  // the theme already applied by ThemeHead before paint. Wrapped so a
  // transient icon error can never break theme state.
  try {
    syncIcons(current());
  } catch (e) {
    /* non-fatal: theme still applies; only the icon glyph may flicker */
  }
})();

// Reinitialize Basecoat components after Datastar DOM morphs (debounced)
(function() {
  var _bcTimer = null;
  var initBasecoat = function() {
    if (typeof basecoat !== 'undefined' && basecoat.initAll) {
      try { basecoat.initAll(); } catch(e) { /* silent */ }
    }
  };
  var debouncedInit = function() {
    if (_bcTimer) cancelAnimationFrame(_bcTimer);
    _bcTimer = requestAnimationFrame(initBasecoat);
  };
  // Initial call after DOM ready
  if (document.readyState === 'loading') {
    document.addEventListener('DOMContentLoaded', initBasecoat);
  } else {
    initBasecoat();
  }
  // Watch for new elements after Datastar merges HTML (debounced via rAF)
  var observer = new MutationObserver(debouncedInit);
  if (document.body) {
    observer.observe(document.body, { childList: true, subtree: true });
  } else {
    document.addEventListener('DOMContentLoaded', function() {
      observer.observe(document.body, { childList: true, subtree: true });
    });
  }
})();

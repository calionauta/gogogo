// Shared leading + trailing throttle (whiteboard cursors, notes carets).
// Leading edge fires immediately; calls inside the window collapse to ONE
// trailing call with the latest args — a throttled update is delayed,
// never dropped. Dropping (throttle without trailing) is exactly what made
// peer carets freeze: click twice within the window and the second position
// vanished with no retry. No dependencies, no build step; classic script
// defining window.GogogoThrottle so feature scripts (also classic, also
// deferred) can use it in document order.
(function () {
  "use strict";

  // throttleTrailing(fn, ms): leading call runs at once, later calls in
  // the window replace the single pending trailing call (latest args win).
  function throttleTrailing(fn, ms) {
    var last = 0;
    var timer = null;
    var latest = null;
    function invoke() {
      timer = null;
      last = Date.now();
      var args = latest;
      latest = null;
      fn.apply(null, args);
    }
    return function () {
      latest = arguments;
      var now = Date.now();
      if (!last || now - last >= ms) {
        if (timer) {
          clearTimeout(timer);
          timer = null;
        }
        invoke();
      } else if (!timer) {
        timer = setTimeout(invoke, ms - (now - last));
      }
    };
  }

  window.GogogoThrottle = { throttleTrailing: throttleTrailing };
})();

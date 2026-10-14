// sse-open.js — verified SSE open, shared by every Datastar stream opener.
//
// Why this exists: the opener clicks a hidden Datastar @get it cannot
// observe. Clicking before Datastar binds data-on:click is a silent no-op
// that ALSO poisons dataset.opened, so the stream never opens — observed
// live as a swap timeout with the server healthy. This helper clicks, then
// polls /api/stream/status until the hub reports the tab registered,
// re-clicking while unregistered; it resolves on open and throws loudly
// past the bound. A dead stream is therefore always either fixed or a loud
// error, never silent.
//
// Plain fetch + chained setTimeout only (never setInterval — see
// TestNoNewVanillaJS). One in-flight verification per button: concurrent
// callers (datastar-ready + fallback + online/visibility re-open) share
// the promise instead of racing clicks.
//
// Upstream watch: starfederation/datastar#1203 tracks silent SSE death
// with no native restart primitive as of v1.2.2. Recheck it before
// building more machinery here — a native open-ack or restart handle
// would replace the status poll below.
(function () {
	var inFlight = {};
	function statusURL() {
		return "/api/stream/status?clientID=" + encodeURIComponent(window.__gogogoClientID || "");
	}
	function sleep(ms) {
		return new Promise(function (resolve) { setTimeout(resolve, ms); });
	}
	async function registered() {
		try {
			var res = await fetch(statusURL(), { headers: { Accept: "application/json" } });
			if (!res.ok) return false;
			var body = await res.json();
			return !!(body && body.open);
		} catch (e) {
			return false;
		}
	}
	function loud(p) {
		// Fire-and-forget callers never .catch: rethrow async so a failed
		// verification surfaces as an uncaught error (the smoke suite fails
		// on those) instead of an unhandled rejection nobody reads.
		p.then(undefined, function (err) {
			setTimeout(function () { throw err; }, 0);
		});
		return p;
	}
	// __gogogoOpenVerified(buttonId) resolves when the hub reports this
	// tab registered, re-clicking the opener until then. Bound: 20
	// attempts x 500ms (~10s) — past that the page is genuinely broken
	// (Datastar or network down) and loud failure beats silent death.
	// window.__gogogoSSEOpenState tracks idle/opening/open/failed for
	// tests and debugging.
	window.__gogogoSSEOpenState = "idle";
	window.__gogogoOpenVerified = function (buttonId) {
		if (inFlight[buttonId]) return inFlight[buttonId];
		var p = (async function () {
			var attempts = 0, maxAttempts = 20, delayMs = 500;
			for (;;) {
				var b = document.getElementById(buttonId);
				try {
					if (b) { b.dataset.opened = "1"; b.click(); }
					if (await registered()) {
						window.__gogogoSSEOpenState = "open";
						return true;
					}
				} catch (e) { /* transient: retry below */ }
				attempts++;
				if (attempts >= maxAttempts) {
					window.__gogogoSSEOpenState = "failed";
					throw new Error("sse: stream for #" + buttonId + " never registered after " + maxAttempts + " attempts (Datastar or network down?)");
				}
				if (b) b.dataset.opened = "";
				window.__gogogoSSEOpenState = "opening";
				await sleep(delayMs);
			}
		})();
		inFlight[buttonId] = p;
		var cleanup = function () { delete inFlight[buttonId]; };
		p.then(cleanup, cleanup);
		return loud(p);
	};
	// Re-open after a disconnect, through the same verification (not a
	// blind click: a torn-down stream never clears dataset.opened, and a
	// pre-binding click poisons it — both are covered by re-verifying).
	window.__gogogoSSEReopen = function (buttonId) {
		var id = buttonId || "sse-opener";
		var b = document.getElementById(id);
		if (b) b.dataset.opened = "";
		return window.__gogogoOpenVerified(id);
	};
})();

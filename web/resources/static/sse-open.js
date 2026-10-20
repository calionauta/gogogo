// sse-open.js — verified SSE open, shared by every Datastar stream opener.
//
// Why this exists: the opener clicks a hidden Datastar @get it cannot
// observe. Clicking before Datastar binds data-on:click is a silent no-op,
// so the stream never opens — observed live as a swap timeout with the
// server healthy.
//
// The proof of an open is the stream's OWN first frame. Every Datastar
// stream starts with an ack signal (internal/datastar.AckStreamOpened:
// signal "streamOpen", value = this tab's clientID). Waiting for it proves
// the click took, the request reached the handler, AND the browser applied
// the response — where an HTTP status probe only proved server-side
// registration, cost a second request per open, and said nothing about
// delivery. No polling: the ack rides the connection the page is opening.
//
// What Datastar owns (v1.0.4, pinned) — all of it configured on the opener,
// not here:
//   - reconnect with backoff after BOTH a clean server close and a network
//     error (`retry: 'always'`, #1073, shipped in v1.0.0-RC.7): retry count
//     resets on every 200, so a deploy loop reconnects instead of dying;
//   - `Last-Event-ID` sent on every reconnect (readStream feeds it back);
//   - close on hidden / reopen on visible (`openWhenHidden` default false
//     for GET; the room opts in because its presence IS the connection);
//   - cancellation by method+URL (`requestCancellation: 'auto'`), which is
//     what makes the forced refresh below safe on a zombie socket.
//
// So this file only covers what the framework cannot see:
//   1. the initial bind race (ack loop, bounded, loud on failure), and
//   2. a FORCED refresh on `online` / `resume` / bfcache `pageshow`, where
//      mobile deep-sleep can leave a fetch that never rejects (#1203: the
//      maintainer's own advice is to re-issue the action from those events).
//      A re-click to the same URL cancels the zombie (requestCancellation)
//      and opens a fresh one; the ack tells us it landed.
//
// Upstream watch: #1203 (open, no milestone) asks for a native restart
// handle; it would replace the forced-refresh half. It does NOT ask for an
// open ack — that half has no upstream issue and is not expected to change.
(function () {
	var inFlight = {};
	var live = {}; // buttonId -> request started and not finished
	var SIGNAL = "streamOpen";
	var ACK_WAIT_MS = 500;
	var MAX_ATTEMPTS = 20;
	var REFRESH_WAIT_MS = 2000;

	function tagged(detail, clientID) {
		var v = detail && detail[SIGNAL];
		if (typeof v !== "string" || v === "") return false;
		// An empty clientID means the page skipped its bootstrap; accepting
		// the ack still proves the stream delivered, so do not strand it.
		return clientID === "" || v === clientID;
	}

	// waitAck resolves true when the stream's ack signal arrives, false on
	// timeout or on a Datastar fetch error for this opener.
	function waitAck(button, clientID, timeoutMs) {
		return new Promise(function (resolve) {
			var done = false;
			var timer = setTimeout(function () { finish(false); }, timeoutMs);
			function finish(ok) {
				if (done) return;
				done = true;
				clearTimeout(timer);
				document.removeEventListener("datastar-signal-patch", onSignal);
				document.removeEventListener("datastar-fetch", onFetch);
				resolve(ok);
			}
			function onSignal(e) {
				if (tagged(e.detail, clientID)) finish(true);
			}
			function onFetch(e) {
				var d = e.detail || {};
				if (d.type === "error" && button && d.el === button) finish(false);
			}
			document.addEventListener("datastar-signal-patch", onSignal);
			document.addEventListener("datastar-fetch", onFetch);
		});
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

	// Track lifecycle for the open state and for nothing else — reconnects
	// are Datastar's job now (retry: 'always').
	document.addEventListener("datastar-fetch", function (e) {
		var d = e.detail || {};
		var id = d.el && d.el.id;
		if (!id) return;
		if (d.type === "started") live[id] = true;
		else if (d.type === "finished" || d.type === "error") live[id] = false;
	});

	// window.__gogogoSSEOpenState tracks idle/opening/open/closed/failed for
	// tests and debugging.
	window.__gogogoSSEOpenState = "idle";

	// __gogogoOpenVerified(buttonId) resolves when the stream acknowledged
	// this tab, re-clicking while the ack is missing. Bound: 20 attempts x
	// 500ms (~10s) — past that the page is genuinely broken (Datastar or
	// network down) and loud failure beats silent death.
	window.__gogogoOpenVerified = function (buttonId) {
		if (live[buttonId] && window.__gogogoSSEOpenState === "open") {
			return Promise.resolve(true);
		}
		if (inFlight[buttonId]) return inFlight[buttonId];
		var p = (async function () {
			var attempts = 0;
			var clientID = window.__gogogoClientID || "";
			for (;;) {
				var b = document.getElementById(buttonId);
				var ack = waitAck(b, clientID, ACK_WAIT_MS);
				try {
					if (b) b.click();
				} catch (e) { /* transient: retry below */ }
				if (await ack) {
					window.__gogogoSSEOpenState = "open";
					return true;
				}
				attempts++;
				if (attempts >= MAX_ATTEMPTS) {
					window.__gogogoSSEOpenState = "failed";
					throw new Error("sse: stream for #" + buttonId + " never acknowledged after " + MAX_ATTEMPTS + " attempts (Datastar or network down?)");
				}
				window.__gogogoSSEOpenState = "opening";
			}
		})();
		inFlight[buttonId] = p;
		var cleanup = function () { delete inFlight[buttonId]; };
		p.then(cleanup, cleanup);
		return loud(p);
	};

	// __gogogoSSERefresh(buttonId) is the FORCED reopen for socket-death
	// cases Datastar cannot observe: `online`, Chromium mobile `resume`, and
	// a bfcache `pageshow`. It re-clicks even a "live" opener, because a
	// deep-slept socket is exactly the case where the fetch never settled
	// (#1203); the same-URL cancel is what kills the zombie. Quiet on
	// failure: Datastar's own retry loop and the next event still apply, so
	// a transient miss here must not throw.
	window.__gogogoSSERefresh = function (buttonId) {
		if (document.hidden) return Promise.resolve(false);
		var id = buttonId || "sse-opener";
		var b = document.getElementById(id);
		if (!b) return Promise.resolve(false);
		var ack = waitAck(b, window.__gogogoClientID || "", REFRESH_WAIT_MS);
		try {
			b.click();
		} catch (e) {
			return Promise.resolve(false);
		}
		return ack;
	};
})();

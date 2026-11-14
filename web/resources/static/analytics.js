// Declarative affordance tracking: one listener, only the elements that opt in.
//
// Markup names the event with data-analytics-event; the server revalidates the
// name, attributes it to the session, and publishes it on the domain-event bus.
// Server-side events never come from here, and this script never polls.
(function () {
	"use strict";
	if (!window.fetch || !document.addEventListener) {
		return;
	}
	document.addEventListener(
		"click",
		function (event) {
			var el = event.target && event.target.closest
				? event.target.closest("[data-analytics-event]")
				: null;
			if (!el) {
				return;
			}
			var name = el.getAttribute("data-analytics-event");
			if (!name) {
				return;
			}
			window
				.fetch("/api/analytics", {
					method: "POST",
					credentials: "same-origin",
					headers: { "Content-Type": "application/json" },
					body: JSON.stringify({ name: name, path: window.location.pathname }),
				})
				.catch(function () {
					// A beacon is best-effort: a dropped one must never surface as
					// an error to the user or block the navigation.
				});
		},
		{ passive: true },
	);
})();

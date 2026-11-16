// The todo list's Datastar wiring, in the slice that owns it.
//
// These helpers used to live in internal/ui, beside the shared `Segmented`
// control. `Segmented` is genuinely shared — the filter rows and the state
// panel's tabs both use it — but the click expressions below are not: they
// spell todo's query strings, its scope/filter signals and its offline
// restore. Keeping them in core meant `internal/ui` knew a slice's routes, so
// a product that dropped the slice still shipped five /api/todos references
// and a test that could no longer find them.
package components

// TodoScopeOn builds the click expression for one todo scope option.
// The query-string composition exists once — the old markup repeated it per
// tab and the copies had already drifted (only some carried `q=`).
func TodoScopeOn(value string) string {
	return TodoListQuery("'/api/todos?filter=' + encodeURIComponent($filter || 'all') + "+
		"'&scope="+value+"&q=' + encodeURIComponent($searchQuery || '') + "+
		"'&clientID=' + encodeURIComponent($clientID || '')",
		todoViewExpr("$filter || 'all'", "'"+value+"'", "$searchQuery || ''"),
		"$scope = '"+value+"'; ")
}

// TodoFilterOn is the status-filter counterpart of TodoScopeOn.
func TodoFilterOn(value string) string {
	return TodoListQuery("'/api/todos?filter="+value+"&scope=' + encodeURIComponent($scope || 'all') + "+
		"'&q=' + encodeURIComponent($searchQuery || '') + "+
		"'&clientID=' + encodeURIComponent($clientID || '')",
		todoViewExpr("'"+value+"'", "$scope || 'all'", "$searchQuery || ''"),
		"$filter = '"+value+"'; ")
}

// todoViewExpr is the view tuple the fragment cache keys on: the same four
// inputs the server's list query takes. One spelling, so no control can cache
// under a view it does not actually show.
func todoViewExpr(filter, scope, q string) string {
	return "{filter: " + filter + ", scope: " + scope + ", q: " + q +
		", after: $createdAfter || ''}"
}

// TodoListQuery wraps a list request with the offline behaviour. The list is
// SERVER-side, so offline the control has exactly two honest moves: show the
// CACHED fragment for the view it asked for (the feature's restorer labels it
// with its age), or say that the view cannot change. It never leaves
// `$loading` set on a request that was refused — nothing clears it on failure,
// and a stuck spinner disabled the composer over a view that never changed.
//
// applyExpr runs ONLY when the cached view was restored: a miss must not leave
// the tab, the scope or the box claiming a view the list is not showing.
func TodoListQuery(urlExpr, viewExpr, applyExpr string) string {
	return "if (!navigator.onLine) { " +
		"if (window.GogogoTodoViews && window.GogogoTodoViews.restore(" + viewExpr + ")) { " + applyExpr + "$listOffline = false } " +
		"else { $listOffline = true } " +
		"} else { $loading = true; @get(" + urlExpr + ", " + listRequestOpts + ") }"
}

// listRequestOpts keeps a list request small. The URL already carries
// filter/scope/query/clientID, so Datastar's default signal blob is dead weight
// — it rode every GET as a `datastar=` param and put the whole todos array in the
// URL. Retries stay at the default: a list GET is idempotent and a transient 5xx
// is exactly what the retry is for (unlike the badge, which a viewer can only
// ever 403).
const listRequestOpts = "{filterSignals: {exclude: /.*/}}"

// TodoScopeSelectOn is the scope <select>'s change handler. It differs from
// the tabs in one way: the select is not bound to the signal, so a refused
// change must also put the control back where the list actually is.
func TodoScopeSelectOn() string {
	return "if (!navigator.onLine) { " +
		"if (window.GogogoTodoViews && window.GogogoTodoViews.restore(" +
		todoViewExpr("$filter || 'all'", "evt.target.value", "$searchQuery || ''") + ")) { $scope = evt.target.value; $listOffline = false } " +
		"else { evt.target.value = $scope || 'all'; $listOffline = true } } " +
		"else { $scope = evt.target.value; $loading = true; @get('/api/todos?filter=' + " +
		"encodeURIComponent($filter || 'all') + '&scope=' + encodeURIComponent($scope || 'all') + " +
		"'&q=' + encodeURIComponent($searchQuery || '') + " +
		"'&clientID=' + encodeURIComponent($clientID || ''), " + listRequestOpts + ") }"
}

// TodoSearchOn is the search box's DEBOUNCED request, and offline it refuses
// instead of restoring a cached view. The debounce is the reason: it fires
// 300ms after the last keystroke, so by then the user may have created or
// deleted a row — and a restore replaces the whole list, which would erase that
// work from the screen (measured: the queued create's row vanished between the
// replay and the delete click). A view switch is an immediate click and can
// restore; a delayed query cannot.
func TodoSearchOn() string {
	return "if (!navigator.onLine) { $listOffline = true } else { $loading = true; @get(" +
		"'/api/todos/search?q=' + encodeURIComponent($searchQuery || '') + " +
		"'&filter=' + encodeURIComponent($filter || 'all') + " +
		"'&scope=' + encodeURIComponent($scope || 'all') + " +
		"'&mode=' + encodeURIComponent(searchModeValue()) + " +
		"'&clientID=' + encodeURIComponent($clientID || ''), " + listRequestOpts + ") }"
}

// TodoSearchModeOn is the search-layer picker's request.
func TodoSearchModeOn() string {
	return TodoSearchOn()
}

// TodoSearchClearOn clears the box and asks for the unfiltered list. Offline it
// refuses BOTH halves: clearing the text while the list still shows the last
// search would be a second lie, so the text stays until the connection does.
func TodoSearchClearOn() string {
	return "if (!navigator.onLine) { " +
		"if (window.GogogoTodoViews && window.GogogoTodoViews.restore(" +
		todoViewExpr("$filter || 'all'", "$scope || 'all'", "''") + ")) { $searchQuery = ''; $listOffline = false } " +
		"else { $listOffline = true } } " +
		"else { $searchQuery = ''; $loading = true; " +
		"@get('/api/todos?filter=' + encodeURIComponent($filter || 'all') + " +
		"'&scope=' + encodeURIComponent($scope || 'all') + " +
		"'&clientID=' + encodeURIComponent($clientID || ''), " + listRequestOpts + ") }"
}

package components

import (
	"bytes"
	"context"
	"strings"
	"testing"

	"github.com/calionauta/gogogo/features/todo"
)

// TestRender_TodoItemCheckboxReflectsCompleted is a regression guard for the
// inverted-checkbox bug: the demo loaded with one task, but toggling it
// surfaced several others because the checkbox state was rendered
// inverted / the create form double-submitted. The checkbox must be
// server-rendered from item.Completed so the UI matches the data on
// first paint (no client-side flip, no phantom rows).
func TestRender_TodoItemCheckboxReflectsCompleted(t *testing.T) {
	done := todo.Todo{ID: "abc123", Title: "finished", Completed: true}
	open := todo.Todo{ID: "def456", Title: "still open", Completed: false}

	var bDone, bOpen bytes.Buffer
	if err := TodoItem(done).Render(context.Background(), &bDone); err != nil {
		t.Fatalf("render done item: %v", err)
	}
	if err := TodoItem(open).Render(context.Background(), &bOpen); err != nil {
		t.Fatalf("render open item: %v", err)
	}
	if !strings.Contains(bDone.String(), "checked") {
		t.Errorf("completed todo must render a checked checkbox; got:\n%s", bDone.String())
	}
	if strings.Contains(bOpen.String(), "checked") {
		t.Errorf("open todo must NOT render a checked checkbox; got:\n%s", bOpen.String())
	}
	// The toggle posts to the item's own id — a regression that pointed
	// at a shared/hardcoded id would double-toggle or hit the wrong row.
	if !strings.Contains(bDone.String(), "/api/todos/abc123/toggle") {
		t.Errorf("done item toggle must target its own id (abc123)")
	}
	if !strings.Contains(bOpen.String(), "/api/todos/def456/toggle") {
		t.Errorf("open item toggle must target its own id (def456)")
	}
}

// TestRender_TodoItemServerStampedAffordances pins the single-home rule:
// delete + authorship affordances render from the item's server-stamped
// IsMine/CanDelete, never from template-side role comparisons. A shared
// row owned by someone else renders the badge + author with a disabled
// trash; an owned row renders the modal-opening trash.
func TestRender_TodoItemServerStampedAffordances(t *testing.T) {
	shared := todo.Todo{
		ID: "s1", Title: "team board", Visibility: todo.VisibilityPublic,
		Owner: "u1", OwnerEmail: "mate@example.com", IsMine: false, CanDelete: false,
	}
	var bShared bytes.Buffer
	if err := TodoItem(shared).Render(context.Background(), &bShared); err != nil {
		t.Fatalf("render shared item: %v", err)
	}
	html := bShared.String()
	for _, want := range []string{"Shared</span>", "mate@example.com", `aria-disabled="true"`} {
		if !strings.Contains(html, want) {
			t.Errorf("shared foreign row must render %q", want)
		}
	}
	if strings.Contains(html, "showModal()") {
		t.Error("shared foreign row must not render the modal-opening trash")
	}

	mine := todo.Todo{
		ID: "m1", Title: "my task", Visibility: todo.VisibilityPrivate,
		Owner: "me", IsMine: true, CanDelete: true,
	}
	var bMine bytes.Buffer
	if err := TodoItem(mine).Render(context.Background(), &bMine); err != nil {
		t.Fatalf("render own item: %v", err)
	}
	if !strings.Contains(bMine.String(), "showModal()") {
		t.Error("owned row must render the modal-opening trash")
	}
	if strings.Contains(bMine.String(), "Shared</span>") {
		t.Error("private row must not render the Shared badge")
	}
}

// TestRender_MutationURLsPreserveSlice pins the template half of the
// tab-slice contract: every list-affecting endpoint (toggle, clear,
// delete-confirm, realtime resync) carries filter + scope (+ clientID
// where the handler needs originator exclusion), in both skins.
// Red-proof: drop &scope= from any URL and this fails naming it.
func TestRender_MutationURLsPreserveSlice(t *testing.T) {
	t.Parallel()
	signals := todo.Signals{
		Filter: "all", Scope: "mine", ItemCount: 1,
		Todos: []todo.Todo{{
			ID: "x1", Title: "probe", Owner: "me",
			Visibility: todo.VisibilityPrivate, IsMine: true, CanDelete: true,
		}},
	}
	var bList, bItem, bSync bytes.Buffer
	if err := TodoList(signals).Render(context.Background(), &bList); err != nil {
		t.Fatalf("render TodoList: %v", err)
	}
	if err := TodoItem(signals.Todos[0]).Render(context.Background(), &bItem); err != nil {
		t.Fatalf("render TodoItem: %v", err)
	}
	// The realtime resync path renders separately (Layout mounts it once
	// outside the list), so it gets its own assertion surface.
	if err := RealtimeRecords().Render(context.Background(), &bSync); err != nil {
		t.Fatalf("render RealtimeRecords: %v", err)
	}
	for _, want := range []string{
		"toggle?filter=",
		"completed/delete?clientID=",
		"confirmingDeleteId + '/delete?clientID=",
	} {
		if !strings.Contains(bList.String(), want) && !strings.Contains(bItem.String(), want) {
			t.Errorf("daisyui markup missing list endpoint carrying slice state: %q", want)
		}
	}
	if !strings.Contains(bSync.String(), "fragment?filter=") {
		t.Error("realtime resync must re-fetch the list fragment")
	}
	pages := []string{bList.String(), bItem.String(), bSync.String()}
	for _, page := range pages {
		for _, endpoint := range []string{"toggle?", "completed/delete?", "/delete?clientID=", "fragment?filter="} {
			idx := strings.Index(page, endpoint)
			if idx < 0 {
				continue
			}
			window := page[idx : idx+320]
			if !strings.Contains(window, "scope=") {
				t.Errorf("endpoint %q must carry &scope= (got %.120q…)", endpoint, window)
			}
		}
	}
}

// TestRender_WorkflowTabFollowsSignal proves the durable-workflow UI is
// server-driven: the tab button, its container, and the empty-state hint
// render only when signals.DagNatsEnabled is true. Rendered twice (on/off)
// so both the enabled path and the trimmed/disabled path are pinned.
func TestRender_WorkflowTabFollowsSignal(t *testing.T) {
	base := todo.Signals{
		Todos:            nil,
		Filter:           "all",
		ItemCount:        0,
		ConnectedClients: 1,
		Suggestions:      []string{},
		SidebarTab:       "queue",
	}
	on := base
	on.DagNatsEnabled = true
	var bOn bytes.Buffer
	if err := TodoStatePanel(on).Render(context.Background(), &bOn); err != nil {
		t.Fatalf("render list (enabled): %v", err)
	}
	htmlOn := bOn.String()
	for _, want := range []string{">Durable Workflow</button>", "Run durable workflow", "/api/onboarding/start"} {
		if !strings.Contains(htmlOn, want) {
			t.Errorf("enabled UI missing %q", want)
		}
	}

	off := base
	off.DagNatsEnabled = false
	var bOff bytes.Buffer
	if err := TodoStatePanel(off).Render(context.Background(), &bOff); err != nil {
		t.Fatalf("render list (disabled): %v", err)
	}
	htmlOff := bOff.String()
	// Match user-visible UI (button label, run button, route), not HTML
	// comments: the source comment naming the three tabs always renders.
	for _, dead := range []string{">Durable Workflow</button>", "Run durable workflow", "/api/onboarding/start"} {
		if strings.Contains(htmlOff, dead) {
			t.Errorf("disabled UI must not contain %q", dead)
		}
	}
	// The queue demo stays usable: disabling workflows must not take the
	// reference demo with it.
	if !strings.Contains(htmlOff, "Run queue + retry demo") {
		t.Errorf("disabled UI lost the queue demo button")
	}
}

// TestRender_EmptyStateHintFollowsSignal pins the empty-state hint to the
// same signal: without the engine there is no workflow to run, so the
// hint must not point at it.
func TestRender_EmptyStateHintFollowsSignal(t *testing.T) {
	var bOff bytes.Buffer
	if err := TodoListRegion(todo.Signals{}).Render(context.Background(), &bOff); err != nil {
		t.Fatalf("render region (disabled): %v", err)
	}
	if strings.Contains(bOff.String(), "Run durable workflow") {
		t.Errorf("disabled empty state must not mention the workflow")
	}
	on := todo.Signals{}
	on.DagNatsEnabled = true
	var bOn bytes.Buffer
	if err := TodoListRegion(on).Render(context.Background(), &bOn); err != nil {
		t.Fatalf("render region (enabled): %v", err)
	}
	if !strings.Contains(bOn.String(), "Run durable workflow") {
		t.Errorf("enabled empty state missing the workflow hint")
	}
}

// TestRender_QueueRetryButtonEnabled is a regression guard for the
// "suggest simulated button came disabled" bug. The Queue + Retry demo
// button must render ENABLED (no static disabled attribute) so the
// goqite + retry-go demo is usable out of the box. Only signal-driven
// disabling (e.g. while the demo runs) is acceptable. In the current UI
// the button lives on the "Queue + Retry" tab and reads
// "Run queue + retry demo".
func TestRender_QueueRetryButtonEnabled(t *testing.T) {
	signals := todo.Signals{
		Todos:            nil,
		Filter:           "all",
		ItemCount:        0,
		ConnectedClients: 1,
		Suggestions:      []string{},
		SimulatedLLM:     true,
		SidebarTab:       "queue",
	}
	var buf bytes.Buffer
	if err := TodoStatePanel(signals).Render(context.Background(), &buf); err != nil {
		t.Fatalf("render list: %v", err)
	}
	html := buf.String()

	if !strings.Contains(html, "Run queue + retry demo") {
		t.Fatalf("Queue + retry demo button missing from rendered UI")
	}
	// The button itself must not carry a hard-coded disabled attribute;
	// only the data-attr:disabled with a *signal* expression is allowed
	// (the create form uses $loading || !$newTitle.trim(), not a literal).
	if strings.Contains(html, `disabled="disabled"`) || strings.Contains(html, `disabled disabled`) {
		t.Errorf("Queue + retry button has a static disabled attribute; it must be enabled by default")
	}
}

// TestRender_ResyncFragmentPreservesSearch is a regression guard for a real
// user-facing bug the browser smoke caught: the resync fragment URL carried
// filter and scope but NOT the search text. The fragment endpoint treats a
// missing q as "no search" (and stores that on the tab's view), so every
// resync — page load, visibility return, doc-version bump, offline sync —
// re-rendered the full list and silently cleared an active search out from
// under the search box. The URL must carry $searchQuery.
func TestRender_ResyncFragmentPreservesSearch(t *testing.T) {
	var buf bytes.Buffer
	if err := RealtimeRecords().Render(context.Background(), &buf); err != nil {
		t.Fatalf("render realtime records: %v", err)
	}
	html := buf.String()
	if !strings.Contains(html, "/api/todos/fragment?") {
		t.Fatalf("resync fragment URL missing from rendered realtime wiring:\n%s", html)
	}
	// The expression is emitted inside a data-on:click string, so the
	// signal read appears as `$searchQuery` rather than a literal q value.
	if !strings.Contains(html, "$searchQuery") {
		t.Error("resync fragment URL omits $searchQuery: a resync would " +
			"re-render the full list and clear the active search")
	}
	// templ HTML-escapes the ampersand, so the emitted form is "&amp;q=".
	if !strings.Contains(html, "q=") {
		t.Error("resync fragment URL omits the q parameter entirely")
	}
}

// TestRender_SearchSeedRidesTheInputNotTheSignals pins the fix for the
// clobbering bug at its source: the URL's ?q= must reach the box through the
// input's own `value=` (which bind.ts turns into the signal with
// {ifMissing: true}), NOT through a server-written `data-signals` entry.
//
// Why both halves matter:
//   - if searchQuery reappears in data-signals, the server is writing a
//     client-owned signal again — the exact bug (six call sites once wrote it,
//     and a re-list mid-typing wiped the box);
//   - if value= disappears, the seed is gone: ?q= no longer opens the box
//     pre-filled, which is why the write existed in the first place.
//
// Red-proof: restore `json:"searchQuery,omitempty"` on Signals.SearchQuery and
// pass a query — the JSON half fails. Remove `value={ signals.SearchQuery }`
// from searchBox — the value= half fails.
func TestRender_SearchSeedRidesTheInputNotTheSignals(t *testing.T) {
	signals := todo.Signals{
		Filter: "all", Scope: "all", SearchEnabled: true,
		SearchQuery: "kestrel",
	}
	var buf bytes.Buffer
	if err := TodoList(signals).Render(context.Background(), &buf); err != nil {
		t.Fatalf("render todo list: %v", err)
	}
	html := buf.String()

	// The signals payload must NOT carry searchQuery: `json:"-"` keeps it off
	// the wire, so no merge can write the client-owned signal. templ escapes
	// the JSON quotes, so the key appears as &#34;searchQuery&#34;.
	if strings.Contains(html, `&#34;searchQuery&#34;`) || strings.Contains(html, `"searchQuery":`) {
		t.Errorf("data-signals carries searchQuery — the server is writing a "+
			"client-owned signal again; a re-list mid-typing would clobber the "+
			"box. Keep it json:\"-\" and seed via value=.\n%s", html)
	}

	// The seed must ride the input's value=.
	if !strings.Contains(html, `value="kestrel"`) {
		t.Errorf("the search input lost its value= seed: ?q= no longer pre-fills "+
			"the box. Expected value=\"kestrel\" in:\n%s", html)
	}

	// The input must still own the signal (data-bind), or value= seeds nothing
	// the debounced handler reads.
	if !strings.Contains(html, `data-bind="searchQuery"`) {
		t.Error("the search input no longer binds $searchQuery: typing would " +
			"not drive the debounced search")
	}
}

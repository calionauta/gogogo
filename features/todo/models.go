// SCOPE:feature — Todo MVC example (reference implementation)
// To remove: delete features/todo/ and the todo roots in buildChiMux
// (internal/server/chi.go).
package todo

import "time"

type Todo struct {
	ID        string    `json:"id"`
	Title     string    `json:"title"`
	Completed bool      `json:"completed"`
	CreatedAt time.Time `json:"created"`
	UpdatedAt time.Time `json:"updated"`
	// Owner is the creating user's id. The UI uses it (with UserID below)
	// to tell "mine" from "shared" and to gate per-row delete affordances.
	Owner string `json:"owner"`
	// OwnerEmail names the author of a shared row so collab partners can
	// tell who wrote what. Empty on owner-scoped reads that need no author.
	OwnerEmail string `json:"ownerEmail"`
	// Visibility is "private" (owner only) or "public" (every login lists
	// it). Listing scope only — what a role may DO stays in Casbin.
	Visibility string `json:"visibility"`
	// IsMine marks rows owned by the requesting login, so the UI can tell
	// "mine" from "shared" (authorship badges) without shipping the
	// viewer's identity into row-level comparisons.
	IsMine bool `json:"isMine"`
	// CanDelete is the per-row delete affordance, computed server-side by
	// canDeleteRow (viewer never, owner always, admin on any visible row).
	// Display-only — the handler re-enforces on POST.
	CanDelete bool `json:"canDelete"`
}

// IsShared reports whether the row is visible beyond its owner.
func (t Todo) IsShared() bool { return t.Visibility == VisibilityPublic }

// Visibility values for Todo.Visibility. The DB CHECK constraint mirrors
// this pair, so a third value fails at the store, not in the UI.
const (
	VisibilityPrivate = "private"
	VisibilityPublic  = "public"
)

// Signals is the todo page's signal set. ONE struct serves three writers:
// the page render (data-signals seed), the stream handshake, and the
// list/search/fragment responses that merge it into a live tab.
//
// THE INVARIANT THAT BIT US TWICE: `json:"-"` marks a field the browser owns.
// A page-level response merges the WHOLE struct, so any field a handler cannot
// know is written at its zero value — and zero is never distinguishable from
// "really false": `sessionAuthed:false` raised the session banner on a
// signed-in page, `clientID:""` sent every later mutation with no client id
// (echo suppression and worker feedback lost their target), `connectedClients:0`
// dropped the presence pill, `sidebarTab:""` blanked the right sidebar, and the
// demo steppers plus the date filter reset on every filter click. Those fields
// carry `json:"-"` and are seeded by the page (components.marshalSignals) and
// by the stream handshake (handlers.mergeHandshake), which are the two places
// that actually know them. A NEW per-tab field belongs here with `json:"-"`,
// not with a tag — the handlers that drive it through MergeSignals maps still
// write it on the wire (encoded/decoded by name, not by this struct).
type Signals struct {
	Todos     []Todo `json:"todos"`
	NewTitle  string `json:"newTitle,omitempty"`
	Filter    string `json:"filter"` // "all", "active", "completed"
	EditingID string `json:"-"`
	EditTitle string `json:"-"`
	Loading   bool   `json:"loading"`
	ItemCount int    `json:"itemCount"`

	// SearchQuery is the text in the search box, and SearchEnabled says
	// whether a search layer is wired at all (false when the plugin was
	// trimmed, which hides the input). `json:"-"` keeps it OUT of
	// data-signals on purpose: the input owns the signal (data-bind) and seeds
	// it from `value=` on first render, so the server never writes it and a
	// re-list cannot clobber the user's typing.
	SearchQuery   string `json:"-"`
	SearchEnabled bool   `json:"searchEnabled"`

	// CreatedAfter is a client-owned date filter (YYYY-MM-DD). Empty means no
	// filter. Every rendered row carries data-created, so filtering is a
	// display decision and never needs a server round trip.
	CreatedAfter string `json:"-"`

	// ListOffline is client-owned (json:"-"): the toolbar sets it when a
	// filter, scope, search or mode change was REFUSED because the browser is
	// offline. The list is server-side, so firing the request would fail and
	// leave $loading stuck (disabling the composer) over a view that never
	// changed — the hint next to the controls reads this instead.
	ListOffline bool `json:"-"`

	NoMatches bool `json:"noMatches"`

	// SessionAuthed is NOT a field here, and that is deliberate. The todo page
	// carries the banner state on its OWN stream (no second long-lived
	// connection; browsers cap connections per host) and revalidates it on the
	// stream heartbeat. It cannot ride Signals: every list/search/fragment
	// response merges the whole struct, so a zero-valued bool serialized as
	// `sessionAuthed:false` into every one of them — picking a different Match
	// mode, or clicking a filter tab, told the shared banner the session had
	// expired and it stayed up until a reload. Only the stream handshake and
	// its heartbeat know the answer, so only they write the signal
	// (features/todo/handlers/stream.go).

	// SearchMode is the ACTIVE layer (substring/fuzzy/bm25) and SearchModes
	// the wired ones, in presentation order, for the selector.
	//
	// Both are server-written signals (no `json:"-"`): unlike the query text,
	// the mode is a server decision — the operator picks the default and the
	// server falls back when a requested layer is unwired — so the server is
	// the authority and the select just reflects it. A select is also a
	// server-rendered control, not a `data-bind` text box: it fires a request
	// on change rather than mirroring a signal, so no clobber hazard exists.
	SearchMode  string   `json:"searchMode,omitempty"`
	SearchModes []string `json:"searchModes,omitempty"`

	// ConfirmDelete is the id of the todo whose delete-confirm modal is
	// currently open. Empty string means no modal is shown. The title
	// is mirrored into ConfirmDeleteTitle so the modal body can name the
	// row without re-fetching it.
	ConfirmDelete      string `json:"-"`
	ConfirmDeleteTitle string `json:"-"`
	Deleting           bool   `json:"-"`

	// TechStep + TechDone + TechPhase drive the techstack diagnostics
	// <ul class="steps"> component. TechStep is the slug of the most
	// recent action ("suggest", "retry-demo", "workflow"); TechDone
	// flips true once success arrives; TechPhase surfaces "error".
	TechStep  string `json:"-"`
	TechDone  bool   `json:"-"`
	TechPhase string `json:"-"`
	// DemoStep drives the progressive lighting of the Queue + Retry demo
	// steps (1 = enqueued, 2 = running/retrying, 3 = completed). It is
	// advanced by the per-attempt SSE "retry" feedback so the steps
	// light one-by-one with a visible gap between attempts instead of
	// all flipping at once when the job finishes.
	DemoStep int `json:"-"`

	// AIStep, AIPending, AIPhase drive the *AI Suggest* stepper, kept
	// deliberately separate from the Queue + Retry demo stepper
	// (TechStep/SuggestPending/TechPhase). They were previously shared,
	// which made running the Queue + Retry demo light up the AI Suggest
	// tab's "Suggestions ready" status erroneously. Seeded so the
	// stepper is inert on first paint.
	AIStep    int    `json:"-"`
	AIPending bool   `json:"-"`
	AIPhase   string `json:"-"`
	// ClientID is the SSE client id assigned when the stream opens. The
	// UI uses it to route queued-job results and retry feedback back to
	// the correct browser tab, so a Suggest triggered here lands here
	// rather than broadcasting to every connected client.
	//
	// NOTE: the JSON key is intentionally "clientID" (not the
	// tagliatelle-preferred "clientId") — it is a public Datastar signal
	// name bound by the frontend, so renaming would break the wire contract.
	//
	// `json:"-"` IS THE POINT: a list/search/mutation response merges this
	// whole struct into the tab's signals, and a handler serving a filter
	// click cannot know the tab's identity — marshalling it there wrote
	// `clientID:""` and every later mutation went out with an empty id (echo
	// suppression and worker feedback both address a client that no longer
	// exists). Connection-owned signals are seeded by the page render
	// (marshalSignals) and written afterwards only by the stream handshake
	// and the presence broadcasts (broadcastPresence, connectedClients only).
	ClientID string `json:"-"`
	// LLMEnabled reflects whether the server was started with a
	// non-empty GOAI_API_KEY. When true, the UI renders the "Suggest"
	// button. When false, the entire AI pathway is hidden.
	LLMEnabled bool `json:"llmEnabled"`

	// SimulatedLLM reflects whether the server was started with
	// SIMULATE_LLM=true. When true, the UI renders the "Suggest
	// (simulated)" button, which exercises the same queue + retry path
	// as Suggest but against an in-process fake LLM (no API key needed).
	//
	// NOTE: the JSON key is intentionally "simulatedLLM" (not the
	// tagliatelle-preferred "simulatedLlm") — it is a public Datastar
	// signal name bound by the frontend.
	SimulatedLLM bool `json:"simulatedLLM"` //nolint:tagliatelle // public Datastar signal name, do not rename

	// Suggestions is the latest AI-suggested completions, populated
	// by POST /api/todos/suggest. Empty when no suggestions or
	// LLMEnabled is false.
	Suggestions []string `json:"-"`
	// SuggestErr surfaces a human-readable error from the LLM
	// provider (without leaking internals). Empty on success.
	SuggestErr string `json:"-"`

	// SuggestPending is true while a suggest job (simulated or real)
	// is in flight. Drives the button spinner + disabled state so the
	// user can't double-fire. Seeded false so the button is enabled
	// on first paint (Datastar's data-attr:disabled treats an
	// undefined signal as falsy, but seeding it explicitly avoids a
	// one-frame flash of the disabled state).
	SuggestPending bool `json:"-"`

	// ConnectedClients is the number of browser tabs/clients currently
	// connected to the SSE stream. Streamed on connect and on every
	// connect/disconnect so the UI can show realtime presence.
	//
	// `json:"-"` for the same reason as ClientID: it is the HUB's count, not
	// a property of a list response, so marshalling the struct from a filter
	// click wrote `connectedClients:0` and the online pill dropped to
	// "0 online" on a page with a live stream.
	ConnectedClients int `json:"-"`

	// LastItemSource tags the most recent todo mutation as either
	// "self" (the user who triggered the HTTP request) or "remote"
	// (a broadcast from another client). The TodoItem template uses
	// this to pick an entry animation (top-down for self, left-slide +
	// pulse for remote) and a highlight tint, so the user can tell at
	// a glance which todos came from them vs. from others. Empty before
	// the first mutation.
	LastItemSource string `json:"-"`

	// DagNatsEnabled reflects whether the durable-workflow UI should
	// render: the engine is enabled AND the onboarding routes were
	// registered at boot (see TodoHandler.dagnatsUIEnabled). False both
	// when DAGNATS_ENABLED=false and when the dagnats unit was trimmed.
	// When true, the UI renders the "Run durable workflow" button.
	DagNatsEnabled bool `json:"dagNatsEnabled"`

	// IsViewer marks the restricted demo role (users.role == 'viewer'):
	// delete affordances render disabled with a message, paid actions
	// hide. Display hint only — enforcement stays in the RBAC policy,
	// so a wrong value degrades to a 403, never to a write.
	IsViewer bool `json:"isViewer"`

	// IsAdmin marks the operator role (users.role == 'admin'): delete
	// affordances render for any visible row, not just owned ones.
	// Display hint only — enforcement stays in the handler, so a wrong
	// value degrades to a 403, never to a write.
	IsAdmin bool `json:"isAdmin"`

	// UserID is the viewer's own owner id, so rows can tell "mine" from
	// "shared" client-side (authorship badges, per-row delete gating).
	UserID string `json:"userId"`

	// Scope selects the listing slice: "all" (mine + shared), "mine", or
	// "shared". Seeded "all"; the scope tabs drive it like $filter.
	Scope string `json:"scope"`

	// PermDenied drives the "your role cannot do that" toast: set when a
	// viewer reaches for a gated affordance. Seeded false; cleared by
	// its close button (same lifecycle as the other ephemeral signals).
	PermDenied bool `json:"-"`

	// SyncFailed drives the create form's "not saved" alert. It is
	// client-owned: the form sets it from a failed request (a 503 from the
	// offline queue, a network error, a refused write) and clears it on the
	// next submit. It is never merged, because the server has no way to know
	// whether the browser stored the mutation.
	SyncFailed bool `json:"-"`

	// ReviewEnabled reflects whether the review-loop UI should render:
	// the review routes are wired AND a real LLM is configured
	// (GOAI_API_KEY set — the review loop is real-LLM only, never
	// simulated). Registration truth, not config truth: false when the
	// review unit was trimmed even if the key is set.
	ReviewEnabled bool `json:"reviewEnabled"`

	// RealtimeKind describes the active broadcast transport so the
	// diagnostics panel can label it accurately: "JetStream" when NATS is
	// enabled and a JetStream context is wired, else the in-process hub.
	RealtimeKind string `json:"-"`

	// StoreLabel describes the active data layer (e.g.
	// "Turso records"). Surfaced in the page sub-header so a
	// tester can verify which persistence mode is running without
	// grep-ing logs.
	StoreLabel string `json:"-"`

	// OfflineLabel describes the offline-sync behavior of the active
	// strategy (e.g. whether SW queues mutations, whether cross-
	// instance sync is on). Surfaced in the page sub-header.
	OfflineLabel string `json:"-"`

	// SidebarTab selects the active async-demo tab in the right sidebar:
	// "queue" (Queue + Retry) or "workflow" (Durable Workflow). Seeded
	// "queue" so the first tab is active on paint; the Durable Workflow
	// tab is only shown when DagNatsEnabled is true.
	//
	// `json:"-"`: the tab choice belongs to the browser. A page-level merge
	// wrote `sidebarTab:""`, which matches no pane — the whole right sidebar
	// went blank after a filter click.
	SidebarTab string `json:"-"`

	// Onboarding progress (DagNats durable workflow). Streamed as the
	// workflow advances so the UI can render a live stepper. Each event
	// carries the current step, total steps, phase, and a human-readable
	// detail. OnboardingActive flips true on the first progress event and
	// stays true so the stepper remains visible until the next run resets
	// it — this is what makes the durable, restart-tolerant nature of the
	// workflow observable in the UI.
	OnboardingActive bool   `json:"-"`
	OnboardingStep   int    `json:"-"`
	OnboardingTotal  int    `json:"-"`
	OnboardingPhase  string `json:"-"`
	OnboardingDetail string `json:"-"`
	// WorkflowCompleted is set true when the durable onboarding
	// workflow finishes all five steps. The UI renders a final alert
	// so the user gets an explicit completion signal (not just the
	// stepper reaching 5/5).
	WorkflowCompleted bool `json:"-"`
}

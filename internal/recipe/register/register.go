// The wiring contract between installed recipe code and the
// composition root.
//
// Why a separate package: the engine (internal/recipe) must stay importable
// without the app's config/auth/store types, and the recipe packages must not
// import internal/server (that would be a cycle, since server imports them
// through the generated registration). This package is the one place both sides
// can name.
//
// The contract is deliberately small. A field here is a promise that every
// recipe may rely on it, so it grows by a decision, not by convenience.
package register

import (
	"context"
	"database/sql"

	natsio "github.com/nats-io/nats.go"

	"github.com/calionauta/gogogo/config"
	"github.com/calionauta/gogogo/internal/auth"
	"github.com/calionauta/gogogo/internal/collab"
	dbgen "github.com/calionauta/gogogo/internal/db/sqlcgen"
	"github.com/calionauta/gogogo/internal/llm"
	"github.com/calionauta/gogogo/internal/natswire"
	"github.com/calionauta/gogogo/internal/queue"
	"github.com/calionauta/gogogo/internal/recipe"
)

// Deps is what an installed recipe may ask the composition root for. The
// composition root builds it from the same values it hands its own handlers, so
// a recipe is not a special case at boot.
type Deps struct {
	Cfg     *config.Config
	Auth    *auth.Auth
	Queries *dbgen.Queries
	Hub     *queue.SSEHub
	// Queue is the goqite work queue the todo slice's suggest/retry handlers
	// register on, and the chat queue the AI slice posts through. Nil in
	// callers that offer no queue — Register must tolerate nil.
	Queue *queue.Queue
	// RealLLM is the provider client the todo slice's suggest, the chat
	// model leg, the review model action and the credits ledger meter.
	// Nil when GOAI_API_KEY is unset — Register must tolerate nil.
	RealLLM *llm.Client
	// SimulatedLLM is the keyless fake the demo paths serve through.
	SimulatedLLM *llm.Client
	// OnShutdown appends a hook the composition root runs at stop (ledger
	// close, actor host stop). Nil in callers with no shutdown — Register
	// must tolerate nil, same nil-safe rule as Slots.
	OnShutdown func(func())
	// SQLDB is the store the composition root migrates. A recipe that owns
	// tables reads it here; the recipe's migration set is applied by the root
	// (recipe.Migrator), never by the recipe itself.
	SQLDB *sql.DB
	// Slots is where a recipe declares its content for a page slot. It is
	// nil in a caller that offers no slots, and Add is nil-safe on the
	// registry, so a recipe registers unconditionally.
	Slots *recipe.SlotRegistry
	// Persister is the shared Loro snapshot store (whiteboard+notes).
	// Nil in callers that offer no collab — Register must tolerate nil
	// by building a local fallback.
	Persister collab.Persister
	// DocStore is the shared Loro doc cache. Nil-safe: Register builds a
	// local store when the root offered none (tests).
	DocStore *collab.DocStore
	// Presence is the shared presence seam both collaborative features
	// use. Nil keeps the handler's own PeerSet fallback.
	Presence collab.PresenceSeam
	// TodoRef is the todo slice's handle holder: Register fills the
	// funcs so the composition root can wire JetStream, onboarding and
	// the DagNats worker without importing the handlers package (the
	// self-mounting gate forbids that import). Nil in callers that do
	// not need the handle — Register must tolerate nil.
	TodoRef *TodoRef
}

// OnboardingResumer is what the todo create path needs from the onboarding
// flow, by method shape only (no import in either direction, same shape
// handlers/service already use). Register carries it as a func arg type
// so the root can wire onboarding without naming todo handlers.
type OnboardingResumer interface {
	ResumeOnboarding(owner string)
}

// TodoRef is the todo handle the Register publishes for the root. Funcs
// close over the handler, so the root never names the concrete type.
type TodoRef struct {
	CreateTodoForOnboarding func(ctx context.Context, title, owner string) error
	SetPublisher            func(pub *natswire.CrudPublisher)
	RunCrudConsumer         func(ctx context.Context, js natsio.JetStreamContext, appName, instanceID string) error
	HealSearch              func(ctx context.Context) error
	SetOnboarding           func(r OnboardingResumer)
}

// Func is what a recipe package exports as `Register`. The generated
// registration file calls it once per installed and not-removed receipt.
//
// A recipe builds its handler here and hands its provider to the seam; it never
// mounts anything itself, so collisions stay the registry's to report.
type Func func(reg *recipe.Registry, d Deps)

// The todo slice mounts itself through the recipe seam.
package handlers

import (
	"context"
	"fmt"

	natsio "github.com/nats-io/nats.go"

	comp "github.com/calionauta/gogogo/features/genui/components"
	"github.com/calionauta/gogogo/internal/natswire"
	"github.com/calionauta/gogogo/internal/queue"
	"github.com/calionauta/gogogo/internal/recipe"
	"github.com/calionauta/gogogo/internal/recipe/register"
	"github.com/calionauta/gogogo/internal/search"
)

// Register builds the todo handler and hands its declaration to the seam.
// Display, LLM, search and RowActions wiring live here so the composition
// root never constructs the handler. Onboarding, JetStream and the DagNats
// worker reach the handler through d.TodoRef (funcs closing over it),
// never through a root import of this package.
func Register(reg *recipe.Registry, d register.Deps) {
	hub := d.Hub
	if hub == nil {
		hub = queue.NewSSEHub()
	}
	h := NewHandler(d.Queries, d.Auth, hub)
	h.SetDatabase(d.SQLDB)
	h.BuildLabel = d.Cfg.BuildLabel
	h.BuildCommit = d.Cfg.BuildCommit
	h.OfflineSyncEnabled = d.Cfg.OfflineSync.Enabled
	h.NATSEnabled = d.Cfg.NATS.Enabled
	h.DemoAccounts = d.Cfg.DemoAccounts
	h.SignupEnabled = d.Cfg.SignupEnabled

	// LLM + queue wiring (suggest/retry handlers). Clients arrive via
	// Deps (created once in BootChi for both todo and chat).
	if d.Queue != nil {
		h.SetQueue(d.Queue)
		h.SetLLMClient(d.RealLLM)
		if d.SimulatedLLM != nil {
			h.SetSimulatedLLMClient(d.SimulatedLLM)
		}
		h.RegisterSuggestHandlers(d.Queue.Registry())
		h.RegisterRetryHandlers(d.Queue.Registry())
	}

	// Search wiring: every layer, mode from config. The heal is deferred
	// via TodoRef.HealSearch (the table lands with the recipe step).
	mode := d.Cfg.Search.Mode
	src := NewTodoSource(d.Queries)
	idx := search.SQLIndex{DB: d.SQLDB}
	if _, err := search.New(mode, src, idx); err != nil {
		panic(fmt.Sprintf("server: search layer %q: %v", mode, err))
	}
	layers := search.NewAll(src, idx)
	set := SearchLayers{
		ByMode:  make(map[string]Searcher, len(layers)),
		Default: mode,
		Order:   search.Modes(),
	}
	for name, layer := range layers {
		set.ByMode[name] = NewTodoSearcher(layer)
	}
	ix := NewTodoIndexer(idx)
	h.SetSearch(set, ix, d.SQLDB)

	h.RowActions = RowActionsFrom(d.Slots)
	// Panel catalog: this unit owns the provider, so it installs the
	// global catalog the chat unit answers through. Chat Register no
	// longer touches panels (it mounts after/before by id; the catalog
	// is process-global and read at request time, after the whole
	// bundled pass has run, so order inside the pass does not matter).
	panels := []comp.PanelProvider{NewTodoPanel(d.Queries, d.Slots)}
	if err := comp.InstallPanelRegistry(panels); err != nil {
		panic(err)
	}
	var actionProviders []comp.ActionProvider
	for _, p := range panels {
		if ap, ok := p.(comp.ActionProvider); ok {
			actionProviders = append(actionProviders, ap)
		}
	}
	if err := comp.InstallActionRegistry(actionProviders); err != nil {
		panic(err)
	}
	reg.Add(h.Recipe())

	if d.TodoRef != nil {
		d.TodoRef.CreateTodoForOnboarding = func(ctx context.Context, title, owner string) error {
			return h.CreateTodoForOnboarding(ctx, title, owner)
		}
		d.TodoRef.SetPublisher = func(pub *natswire.CrudPublisher) {
			h.SetPublisher(pub)
		}
		d.TodoRef.RunCrudConsumer = func(ctx context.Context, js natsio.JetStreamContext, appName, instanceID string) error {
			return RunCrudConsumer(ctx, js, appName, instanceID, h.Q, h.Index, h.DB, h.Bus())
		}
		d.TodoRef.HealSearch = func(ctx context.Context) error {
			return HealSearchIndex(ctx, d.Queries, ix, d.SQLDB)
		}
		d.TodoRef.SetOnboarding = func(r register.OnboardingResumer) {
			h.SetOnboarding(r)
		}
	}
}

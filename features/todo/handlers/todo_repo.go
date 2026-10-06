// SCOPE:layer=feature,removal=feature — Repository helpers for the todo handler (thin wrappers
package handlers

import (
	"context"
	"errors"
	"fmt"

	"github.com/a-h/templ"
	"github.com/pocketbase/pocketbase/core"

	"github.com/calionauta/gogogo/features/store"
	"github.com/calionauta/gogogo/features/store/pbstore"
	"github.com/calionauta/gogogo/features/todo"
	"github.com/calionauta/gogogo/features/todo/components"
	basecoat "github.com/calionauta/gogogo/web/skins/basecoat"
	morpheus "github.com/calionauta/gogogo/web/skins/morpheus"
)

// ErrNoOwner is returned by RequireOwner when the request carries no
// authenticated user. Handlers translate it into a 303 redirect to
// /login (same convention as handleList/handleListFragment) so an
// expired or missing session can never reach the store with an empty
// owner — which would list EVERY user's todos (no owner filter) or
// write ownerless records.
var ErrNoOwner = errors.New("todo: no authenticated owner")

// RequireOwner returns the authenticated user's id, or ErrNoOwner when
// the request is unauthenticated. This is the single choke point for
// tenant scoping: every handler resolves the owner through here (or
// through listTodos/saveTodo/countOwnedTodos below, which call it
// internally), so a future handler cannot accidentally fall back to an
// unscoped store call the way the old ownerOf(c)=="" used to allow.
//
// Callers handle ErrNoOwner INLINE (resolve + redirect in two lines,
// not via a shared helper) because c.Redirect writes the response and
// returns nil — a helper cannot signal "already responded" through its
// error return, and the explicit form keeps the control flow greppable
// at each mutation entry point.
func RequireOwner(c *core.RequestEvent) (string, error) {
	if c == nil || c.Auth == nil {
		return "", ErrNoOwner
	}
	return c.Auth.Id, nil
}

// listTodos returns the authenticated user's todos, scoped by the
// store (the strategy filters by owner internally). The handler-side
// filter values are: "" (all), "active", "completed".
//
// Fail-closed: an unauthenticated call returns ErrNoOwner instead of
// an unscoped list, so even a handler that forgot its auth check
// cannot leak other users' todos.
func (h *TodoHandler) listTodos(c *core.RequestEvent, filter string) ([]todo.Todo, error) {
	owner, err := RequireOwner(c)
	if err != nil {
		return nil, err
	}
	todos, err := h.st().List(ctxOf(c), owner, filter)
	if err != nil {
		return nil, fmt.Errorf("list todos (filter=%q): %w", filter, err)
	}
	return todos, nil
}

// saveTodo persists a new todo owned by owner. idemKey is the
// client-generated UUID used for offline-replay dedup (PBStore uses it
// via the OnRecordCreateRequest hook; CRDTStore would use op IDs).
//
// Fail-fast: an empty owner is rejected here (not written ownerless),
// so a programming error surfaces as an error, not an invisible row.
func (h *TodoHandler) saveTodo(c *core.RequestEvent, item *todo.Todo, owner, idemKey string) error {
	return h.saveTodoCtx(ctxOf(c), item, owner, idemKey)
}

// saveTodoCtx is saveTodo for callers that have no *core.RequestEvent — the
// durable-workflow steps, which run on a DagNats worker goroutine and would
// otherwise have to fabricate a request (or silently write with a nil
// context, which is what CreateTodoForOnboarding used to do).
func (h *TodoHandler) saveTodoCtx(ctx context.Context, item *todo.Todo, owner, idemKey string) error {
	if owner == "" {
		return ErrNoOwner
	}
	out, err := h.st().Create(ctx, *item, owner, idemKey)
	if err != nil {
		return fmt.Errorf("save todo: %w", err)
	}
	*item = out
	return nil
}

// countOwnedTodos returns the number of todos owned by the current
// authenticated user. Cheap — uses the store's count query, no full
// load. Fail-closed like listTodos: no auth, no count.
func (h *TodoHandler) countOwnedTodos(c *core.RequestEvent) (int, error) {
	owner, err := RequireOwner(c)
	if err != nil {
		return 0, err
	}
	return h.st().Count(ctxOf(c), owner)
}

// renderTodoList builds the SSE-friendly HTML for the list region,
// dispatching to the skin-specific list template so a morpheus (or
// basecoat) client receives morpheus (or basecoat) HTML on every
// patch — not the default DaisyUI rows. Without this, filter clicks
// and CRUD mutations replace the morpheus card / neo-checkbox rows
// with DaisyUI rows that no longer match the surrounding morpheus
// chrome (CAL-14). Falls back to the shared DaisyUI component when
// the skin is unrecognised so old behaviour is preserved for
// anything we haven't taught about yet.
func (h *TodoHandler) renderTodoList(todos []todo.Todo, skinName string) templ.Component {
	signals := todo.Signals{
		Todos: todos, Filter: "all", ItemCount: len(todos),
		LLMEnabled: h.llmEnabled(),
	}
	return h.renderTodoListRegion(signals, skinName)
}

// renderTodoListRegion is the Signals-flavoured sibling of
// renderTodoList. handleList / handleListFragment already build their
// own todo.Signals (carrying the current filter etc.) and just need a
// template dispatch.
func (h *TodoHandler) renderTodoListRegion(signals todo.Signals, skinName string) templ.Component {
	switch skinName {
	case SkinMorpheus:
		return morpheus.TodoListRegion(signals)
	case SkinBasecoat:
		return basecoat.TodoListRegion(signals)
	default:
		return components.TodoListRegion(signals)
	}
}

// ctxOf returns a context.Context derived from the request. Falls back
// to context.Background() for synthetic calls (no request, e.g. the
// onboarding worker that calls saveTodo programmatically).
func ctxOf(c *core.RequestEvent) context.Context {
	if c == nil || c.Request == nil {
		return context.Background()
	}
	return c.Request.Context()
}

// st returns the configured store. router.Init calls SetStore
// before the handler serves traffic; st() then returns that store
// directly. If SetStore was never called (test paths that wire
// via handlers.New() but skip SetStore), the first concurrent
// caller initializes a PBStore on demand; subsequent callers
// return that same instance.
//
// sync.Once is the race-condition fix: the previous "if h.store ==
// nil { h.store = ... }" pattern wrote to h.store from concurrent
// request handlers + SSE stream openers, which `-race` caught in CI.
func (h *TodoHandler) st() store.EntityStore[todo.Todo] {
	h.stOnce.Do(func() {
		if h.store == nil {
			h.stFallback = pbstore.New(h.app, "todos")
		}
	})
	if h.store == nil {
		return h.stFallback
	}
	return h.store
}

// Compile-time guard: the handlers package depends on EntityStore
// being wired by router.Init via SetStore. Without it, listTodos /
// saveTodo / countOwnedTodos would panic on first use. pbstore.PBStore
// is the default implementation; the guard uses the concrete type so
// the build fails if PBStore drifts from the interface.
var _ store.EntityStore[todo.Todo] = (*pbstore.PBStore)(nil)

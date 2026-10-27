// SCOPE:feature — notifications: the inbox page and its actions.
//
// Delivery records live in `notifications` (owner-scoped, deduped by
// transaction id). This file owns the HTTP surface: the page, the live stream,
// scheduling a reminder, and marking rows read. The durable delay and the write
// live elsewhere (internal/dagnats for the workflow, remind.go for the worker
// body), so a handler failure can never lose a scheduled reminder.
package handlers

import (
	"context"
	"database/sql"
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"
	sdk "github.com/starfederation/datastar-go/datastar"

	"github.com/calionauta/gogogo/config"
	dshelpers "github.com/calionauta/gogogo/internal/datastar"
	dbgen "github.com/calionauta/gogogo/internal/db/sqlcgen"
	"github.com/calionauta/gogogo/internal/dbschema"
	"github.com/calionauta/gogogo/internal/delivery"

	"github.com/calionauta/gogogo/features/notifications"
	"github.com/calionauta/gogogo/features/notifications/components"
	"github.com/calionauta/gogogo/internal/auth"
)

// listLimit bounds the inbox page. A template demo does not paginate; the row
// count is small enough that a second page would be interface noise.
const listLimit = 50

// RunStarter is the slice of the workflow client this feature needs. It is
// exported so the composition root can inject the DagNats client without this
// package importing the engine: `internal/dagnats` is a trim unit, and a
// feature that imported it would either drag the engine along or dangle after
// the trim.
type RunStarter interface {
	StartRun(ctx context.Context, workflow string, input any) (string, error)
}

// Handler serves the notifications feature on chi.
type Handler struct {
	auth     *auth.Auth
	cfg      *config.Config
	q        *dbgen.Queries
	bus      *notifications.Bus
	delivery *delivery.Router
	runs     RunStarter
}

// NewHandler builds the chi handler. Nil deps panic: wire errors surface at
// boot, not per request. The workflow client is injected later through
// SetRunStarter, because it is built where the engine is (cmd/web).
func NewHandler(
	a *auth.Auth, cfg *config.Config, q *dbgen.Queries,
	bus *notifications.Bus, router *delivery.Router,
) *Handler {
	if a == nil {
		panic("notifications chi: NewHandler with nil auth")
	}
	if cfg == nil {
		panic("notifications chi: NewHandler with nil config")
	}
	if q == nil {
		panic("notifications chi: NewHandler with nil queries")
	}
	if bus == nil {
		panic("notifications chi: NewHandler with nil bus")
	}
	if router == nil {
		panic("notifications chi: NewHandler with nil delivery router")
	}
	return &Handler{auth: a, cfg: cfg, q: q, bus: bus, delivery: router}
}

// SetRunStarter wires the workflow client. Without it the reminder form is not
// rendered and the endpoint refuses, which is what keeps a trimmed engine from
// leaving a button that always fails.
func (h *Handler) SetRunStarter(r RunStarter) { h.runs = r }

// wired reports whether scheduling is possible. It is the single truth the
// page and the endpoint share.
func (h *Handler) wired() bool { return h.runs != nil }

// Routes returns the chi router. Every path is absolute, so MountFeature
// flattens it onto the root without a prefix.
func (h *Handler) Routes() chi.Router {
	r := chi.NewRouter()
	r.Get("/notifications", h.handlePage)
	r.Get("/api/notifications/stream", h.handleStream)
	r.Post("/api/notifications/remind", h.handleRemind)
	r.Post("/api/notifications/read-all", h.handleReadAll)
	r.Post("/api/notifications/{id}/read", h.handleRead)
	return r
}

// handlePage renders the inbox for a signed-in user; guests bounce to login.
func (h *Handler) handlePage(w http.ResponseWriter, r *http.Request) {
	sess, err := h.auth.Session(r)
	if err != nil || sess == nil || sess.User == nil {
		http.Redirect(w, r, "/login?next=/notifications&auth=required", http.StatusSeeOther)
		return
	}
	data, err := h.pageData(r.Context(), sess.User.ID)
	if err != nil {
		h.logError(r, "notifications: load inbox", err)
		http.Error(w, "inbox unavailable", http.StatusInternalServerError)
		return
	}
	// The form's channel list belongs to the page render only: the stream and
	// the read actions patch #inbox, which does not contain the form.
	data.Channels = h.offeredChannels(sess.User.Email)
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	_ = components.Page(sess.User.Email, sess.Role, h.cfg.BuildLabel, h.cfg.BuildCommit, data).
		Render(r.Context(), w)
}

// handleRead marks one row read and patches the inbox region. A row that
// belongs to someone else updates nothing (the query is owner-scoped).
func (h *Handler) handleRead(w http.ResponseWriter, r *http.Request) {
	h.mutate(w, r, func(ctx context.Context, owner string) error {
		_, err := h.q.MarkNotificationRead(ctx, dbgen.MarkNotificationReadParams{
			ReadAt: sql.NullString{String: dbschema.Now(), Valid: true},
			ID:     chi.URLParam(r, "id"),
			Owner:  owner,
		})
		return err
	})
}

// handleReadAll marks every in-app row read for the session owner.
func (h *Handler) handleReadAll(w http.ResponseWriter, r *http.Request) {
	h.mutate(w, r, func(ctx context.Context, owner string) error {
		_, err := h.q.MarkAllNotificationsRead(ctx, dbgen.MarkAllNotificationsReadParams{
			ReadAt: sql.NullString{String: dbschema.Now(), Valid: true},
			Owner:  owner,
		})
		return err
	})
}

// mutate is the shared body of the two read actions: session check, the
// owner-scoped write, one patch of the inbox region for the clicker, and one
// bus event for every other tab of the same owner.
func (h *Handler) mutate(w http.ResponseWriter, r *http.Request,
	write func(ctx context.Context, owner string) error,
) {
	sess, err := h.auth.Session(r)
	if err != nil || sess == nil || sess.User == nil {
		http.Error(w, "auth required", http.StatusUnauthorized)
		return
	}
	owner := sess.User.ID
	if werr := write(r.Context(), owner); werr != nil {
		h.logError(r, "notifications: mark read", werr)
		http.Error(w, "write failed", http.StatusInternalServerError)
		return
	}
	data, derr := h.pageData(r.Context(), owner)
	if derr != nil {
		h.logError(r, "notifications: reload inbox", derr)
		http.Error(w, "inbox unavailable", http.StatusInternalServerError)
		return
	}
	sse := sdk.NewSSE(w, r)
	//nolint:contextcheck // datastar-go sink boundary: RenderAndPatch renders through templ; the SDK takes no ctx.
	if perr := h.patchInboxData(sse, data); perr != nil {
		h.logError(r, "notifications: patch inbox", perr)
		return
	}
	// Wake the owner's other tabs. The clicker is excluded: its own response
	// already carried the same patch, and a second one would be a no-op frame.
	h.bus.Publish(notifications.Event{Owner: owner, Except: r.URL.Query().Get("clientID")})
}

// pageData reads the rows and the unread count for one owner. The two reads
// are deliberately separate: the count is a cheap index lookup and the list
// is bounded, so a large inbox never changes the badge's cost.
func (h *Handler) pageData(ctx context.Context, owner string) (components.PageData, error) {
	rows, err := h.q.ListNotificationsForOwner(ctx, dbgen.ListNotificationsForOwnerParams{
		Owner: owner, Limit: listLimit,
	})
	if err != nil {
		return components.PageData{}, err
	}
	unread, err := h.q.CountUnreadInApp(ctx, owner)
	if err != nil {
		return components.PageData{}, err
	}
	items := make([]components.Item, 0, len(rows))
	for _, row := range rows {
		items = append(items, components.Item{
			ID:      row.ID,
			Subject: row.Subject,
			Body:    row.Body,
			When:    friendlyTime(row.Created),
			Unread:  !row.ReadAt.Valid,
		})
	}
	return components.PageData{
		Items:  items,
		Unread: int(unread),
		Wired:  h.wired(),
	}, nil
}

// offeredChannels is the form's channel list: configured in the router AND
// addressable for this user. A channel with no transport or no address is not
// offered, so a control never ends as skipped because of the form.
func (h *Handler) offeredChannels(userEmail string) []components.ChannelOption {
	if userEmail == "" {
		return nil
	}
	if _, ok := h.delivery.For(delivery.ChannelEmail); !ok {
		return nil
	}
	return []components.ChannelOption{{
		Channel: string(delivery.ChannelEmail),
		Label:   "Also send by e-mail",
	}}
}

// patchInboxData renders the inbox region and patches the shell's unread
// signal. It is the one place the stream and the read actions share, so a live
// push and a click cannot drift, and the sidebar count moves with the rows it
// counts on every page (session.js carries the same value to other tabs).
func (h *Handler) patchInboxData(sse *sdk.ServerSentEventGenerator, data components.PageData) error {
	if err := dshelpers.RenderAndPatch(sse, components.Inbox(data), sdk.WithSelector("#inbox")); err != nil {
		return err
	}
	return dshelpers.MergeSignals(sse, map[string]any{"inboxUnread": data.Unread})
}

// friendlyTime keeps the raw timestamp when it cannot be parsed. A broken
// clock should not blank the page.
func friendlyTime(raw string) string {
	t, err := time.Parse(dbschema.TimeFormat, raw)
	if err != nil {
		return raw
	}
	return t.Local().Format("02 Jan 15:04")
}

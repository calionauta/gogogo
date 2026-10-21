// SCOPE:feature — Room SSE stream (presence by connection, pushed roster)
//
// The room reads as a live roster instead of a 3s poll. Presence is the
// CONNECTION: onConnect joins the grain, onDisconnect leaves it, and the
// grain refcounts tabs so closing one of two does not evict the person.
// That deletes the app-level heartbeat the demo used to need — the SSE
// transport's own keepalive (config.DefaultSSEHeartbeatInterval) forces Go
// to notice a closed socket, which is the liveness signal a grain actually
// wants. A heartbeat was a SECOND, weaker answer to the same question: it
// was idempotent per NAME, so "Leave" could never stick while a timer kept
// reporting the departed member alive. Presence by connection has no timer
// to lose that race against.
//
// Every mutation (join/leave/acquire/release/crash-restart) re-reads the
// roster from the grain and broadcasts the WHOLE snapshot, never a delta:
// a dropped event is healed by the next one, and an EventSource reconnect
// that missed the window is harmless. The client also gets a snapshot in
// onConnect, so a fresh or reconnected tab paints immediately.
package handlers

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"maps"
	"net/http"
	"strings"
	"time"

	"github.com/google/uuid"
	datastarsdk "github.com/starfederation/datastar-go/datastar"

	"github.com/calionauta/gogogo/config"
	roomcomponents "github.com/calionauta/gogogo/features/room/components"
	dshelpers "github.com/calionauta/gogogo/internal/datastar"
	appgoakt "github.com/calionauta/gogogo/internal/goakt"
	"github.com/calionauta/gogogo/internal/observability"
)

// pushRoster re-reads the grain and fans ONE Datastar patch out to every tab
// watching this room: the card region plus the lock signals, in a single
// frame. A read failure is logged, not surfaced: the mutation that triggered
// it already happened, and the next event (or the tab's own reconnect
// snapshot) carries the truth. Returns the roster so request handlers can
// answer the clicker with the same state they broadcast.
//
// The payload is the same patch the clicker receives locally (see
// answerRoster), so "what the clicker rendered" and "what the peers painted"
// are one artifact — the poll era had two renderers and they drifted. Each
// recipient's own lock affordance comes from the merged signals, which
// reference $roomMe (per tab), so no server-side per-recipient render.
func (h *Handler) pushRoster(ctx context.Context, id string) (appgoakt.Roster, error) {
	return h.pushRosterExcept(ctx, id, "")
}

// pushRosterExcept is pushRoster for every tab EXCEPT one. The joiner uses
// it: its own tab gets the card from the personal patch below, and a
// broadcast rendered without identity would briefly drop its "(you)" row.
func (h *Handler) pushRosterExcept(ctx context.Context, id, exceptClientID string) (appgoakt.Roster, error) {
	roster, err := h.readRoster(ctx, id)
	if err != nil {
		return appgoakt.Roster{}, err
	}
	payload, mErr := rosterPatch(ctx, id, roster)
	if mErr != nil {
		obsArgs := append(observability.WithComponent("room", "feature"), "room", id, "error", mErr)
		observability.Warn(ctx, "room: build roster patch", obsArgs...)
		return roster, mErr
	}
	h.hub.BroadcastExceptToDoc(payload, id, exceptClientID)
	return roster, nil
}

// rosterPatch renders the card + lock signals as one SSE frame for a
// BROADCAST recipient (no identity: a push has one body for every tab).
func rosterPatch(ctx context.Context, id string, roster appgoakt.Roster) ([]byte, error) {
	return rosterPatchBuffer(ctx, id, roster, "")
}

// rosterPatchFor is rosterPatch for ONE recipient (non-empty email marks that
// tab's own row and seeds $roomMe).
func rosterPatchFor(ctx context.Context, id string, roster appgoakt.Roster, email string) ([]byte, error) {
	return rosterPatchBuffer(ctx, id, roster, email)
}

// rosterPatchBuffer is the single place the room's SSE payload is produced.
// Both the stream (broadcast) and the POST answer (the clicker) call it, so
// the frame a peer receives and the frame the clicker receives cannot drift.
//
// Frame order matters: SIGNALS before ELEMENTS. Datastar evaluates events in
// arrival order and the card's data-show/data-text expressions are evaluated
// while the element patch is applied — elements first means every lock
// button renders against a stale $roomHolder, which reads as "the click did
// nothing" even though the markup updated.
func rosterPatchBuffer(ctx context.Context, id string, roster appgoakt.Roster, email string) ([]byte, error) {
	var out bytes.Buffer
	if err := sendRosterPatch(ctx, patchWriter(&out), id, roster, email, nil); err != nil {
		return nil, err
	}
	return out.Bytes(), nil
}

// sendRosterPatch emits the signals+card pair into an existing generator.
//
// Uses the SDK's own PatchSignals/PatchElements rather than a hand-built
// Send: the dataline prefixes are constants of the wire protocol
// (`signals `, `elements `, `mode `, `selector `), and spelling them by hand
// produced frames the client accepted but could not interpret — the roster
// rendered while every lock button stayed on its previous visibility.
//
// SIGNALS first: Datastar applies events in arrival order, and the card's
// data-show/data-text expressions are evaluated while the element patch is
// applied. Elements first means the buttons read a stale $roomHolder and the
// click looks like it did nothing.
func sendRosterPatch(
	ctx context.Context, gen *datastarsdk.ServerSentEventGenerator,
	id string, roster appgoakt.Roster, email string, extra map[string]any,
) error {
	signals := roomcomponents.RoomLockSignals(roster, email)
	// extra is the per-viewer addition a CLICKER's answer may carry (the
	// "you left" flag). Broadcasts pass nil: one body for every tab cannot
	// hold a value that is only true for one of them.
	maps.Copy(signals, extra)
	if err := gen.MarshalAndPatchSignals(signals); err != nil {
		return err
	}
	card, err := renderCardContent(ctx, id, roster, email)
	if err != nil {
		return err
	}
	// Patch the CONTENT REGION inside the card, never the card shell. The
	// region is a stable element whose id-matched children (the lock
	// buttons) Datastar's morph preserves, so the data-show effects stay
	// bound to the surviving nodes; the card's own wrapper markup and
	// classes are not part of the payload at all.
	return gen.PatchElements(card,
		datastarsdk.WithSelector("#room-card-content"),
		datastarsdk.WithMode(datastarsdk.ElementPatchModeOuter))
}

// renderCardContent renders the PATCH REGION (the card's inner content).
func renderCardContent(ctx context.Context, id string, roster appgoakt.Roster, email string) (string, error) {
	var buf strings.Builder
	if err := roomcomponents.RoomCardContent(id, roster, email).Render(ctx, &buf); err != nil {
		return "", err
	}
	return buf.String(), nil
}

// patchWriter builds a datastar SSE generator over an in-memory writer, so
// the SDK can produce the event framing without an http.ResponseWriter.
// Request context is Background: this runs while BUILDING a payload for a
// broadcast, not while serving a request, and a payload must not be
// cancelled by whichever tab happened to trigger it.
func patchWriter(w io.Writer) *datastarsdk.ServerSentEventGenerator {
	return datastarsdk.NewSSE(&memResponseWriter{w: w}, &http.Request{})
}

// memResponseWriter is the minimal ResponseWriter the SDK needs when it is
// only generating bytes: headers are discarded and Flush is a no-op.
type memResponseWriter struct{ w io.Writer }

func (m *memResponseWriter) Header() http.Header         { return http.Header{} }
func (m *memResponseWriter) Write(b []byte) (int, error) { return m.w.Write(b) }
func (m *memResponseWriter) WriteHeader(int)             {}
func (m *memResponseWriter) Flush()                      {}

// handleStream opens the room's SSE stream. The room id is sanitized BEFORE
// it reaches the grain or the hub, because the grain sanitizes its actor name
// internally: pushing to the raw id would fan out on a tag no client ever
// registered, and every event would land nowhere while looking healthy.
//
// It registers on the hub but drives its own write loop (the pattern
// features/todo/handlers/stream.go established) instead of using
// hub.ServeStream. ServeStream frames every payload as a single `data:`
// line, which is right for JSON envelopes (notes/whiteboard) but wrong for a
// Datastar patch: that needs its own `event:` line, and a payload wrapped in
// `data:` is a message event Datastar ignores. Here the payload already IS a
// complete SSE frame (produced by datastar-go), so it is written verbatim.
func (h *Handler) handleStream(w http.ResponseWriter, r *http.Request) {
	email, _, ok := h.owner(w, r)
	if !ok {
		return
	}
	id := appgoakt.SanitizeRoomID(roomID(r))
	clientID := r.URL.Query().Get("clientID")
	if clientID == "" {
		clientID = uuid.NewString()
	}

	flusher, canFlush := w.(http.Flusher)
	if !canFlush {
		writeText(w, http.StatusInternalServerError, "streaming unsupported")
		return
	}
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	w.WriteHeader(http.StatusOK)
	fmt.Fprintf(w, ": connected %s\n\n", id) //nolint:gosec // id is SanitizeRoomID output, fixed charset
	flusher.Flush()

	ch := make(chan []byte, config.DefaultClientQueueSize)
	h.hub.Register(clientID, "", ch)
	h.hub.SetDoc(clientID, id)
	defer func() { //nolint:contextcheck // the stream ctx is cancelled; leaveRoom bounds its own
		// RECONNECT GUARD, and it is load-bearing: a reconnect (visibility
		// return, online recovery, a graceful-close retry) re-opens the stream
		// and registers the SAME per-tab clientID. The old handler's deferred
		// cleanup then runs AFTER that, so an unguarded leave announced a
		// departure for a tab that was demonstrably still connected — the user
		// disappeared from their own roster. UnregisterIfCurrent is the
		// authoritative answer ("was I still the registered channel?"), so the
		// domain leave rides on it instead of on a second lookup.
		if !h.hub.UnregisterIfCurrent(clientID, ch) {
			return
		}
		h.dropWatcher(id, clientID)
		h.leaveRoom(email, id, clientID)
	}()

	// Open frames: the ack sse-open.js waits for (after registration, so
	// "ack received" also means "the hub has me"), then the session state —
	// this page does not open the dedicated session stream.
	authed := true
	if err := h.sendOpenFrames(w, clientID, authed); err != nil {
		return
	}
	flusher.Flush()

	// Join, then hand THIS tab the current card before anything else, so a
	// fresh or reconnected tab paints without waiting for a peer's change.
	if tellErr := h.rooms.Tell(r.Context(), id, appgoakt.RoomJoin{Name: email, ClientID: clientID}); tellErr != nil {
		obsArgs := append(observability.WithComponent("room", "feature"), "room", id, "error", tellErr)
		observability.Warn(r.Context(), "room: stream join failed", obsArgs...)
		return
	}
	h.addWatcher(id, clientID, email)
	// One grain read serves both audiences: the peers get the broadcast
	// (without identity), this tab gets the same roster rendered with it.
	roster, pushErr := h.pushRosterExcept(r.Context(), id, clientID)
	if pushErr != nil {
		obsArgs := append(observability.WithComponent("room", "feature"), "room", id, "error", pushErr)
		observability.Warn(r.Context(), "room: push after join failed", obsArgs...)
	}
	if payload, err := rosterPatchFor(r.Context(), id, roster, email); err == nil {
		_, _ = w.Write(payload) //nolint:gosec // server-rendered Templ patch, not user HTML
		flusher.Flush()
	}

	heartbeat := time.NewTicker(config.DefaultSSEHeartbeatInterval)
	defer heartbeat.Stop()
	for {
		select {
		case <-r.Context().Done():
			return
		case <-heartbeat.C:
			// The ticker is what makes Go notice a dead socket: a parked
			// writer never learns the peer is gone, so without this the
			// handler and its hub registration leak forever.
			if _, err := fmt.Fprint(w, ": heartbeat\n\n"); err != nil {
				return
			}
			flusher.Flush()
			authed = h.revalidateSession(w, flusher, r, authed)
		case payload := <-ch:
			if _, err := w.Write(payload); err != nil {
				return
			}
			flusher.Flush()
		}
	}
}

// sendOpenFrames writes the stream's two handshake frames.
func (h *Handler) sendOpenFrames(w io.Writer, clientID string, authed bool) error {
	if err := dshelpers.AckStreamOpened(patchWriter(w), clientID); err != nil {
		return err
	}
	return dshelpers.MergeSignals(patchWriter(w), map[string]bool{"sessionAuthed": authed})
}

// revalidateSession re-checks the request's cookie on the heartbeat and
// patches the banner signal on change (flushing the raw writer). Split from
// the loop for the complexity gate.
func (h *Handler) revalidateSession(w http.ResponseWriter, f http.Flusher, r *http.Request, authed bool) bool {
	_, err := h.auth.Session(r)
	if (err == nil) == authed {
		return authed
	}
	authed = err == nil
	if werr := dshelpers.MergeSignals(patchWriter(w), map[string]bool{"sessionAuthed": authed}); werr != nil {
		return authed
	}
	f.Flush()
	return authed
}

// leaveRoom drops one connection and re-pushes. The grain decides whether
// the MEMBER also leaves (it knows how many other tabs that person has).
func (h *Handler) leaveRoom(email, id, clientID string) {
	// The stream's context is already cancelled here, so this needs a fresh
	// bounded one; without a timeout a stalled grain would park the dying
	// handler's cleanup goroutine forever.
	ctx, cancel := context.WithTimeout(context.Background(), mutateTimeout)
	defer cancel()
	if tellErr := h.rooms.Tell(ctx, id, appgoakt.RoomLeave{Name: email, ClientID: clientID}); tellErr != nil {
		obsArgs := append(observability.WithComponent("room", "feature"), "room", id, "error", tellErr)
		observability.Warn(ctx, "room: stream leave failed", obsArgs...)
		return
	}
	if _, err := h.pushRoster(ctx, id); err != nil {
		obsArgs := append(observability.WithComponent("room", "feature"), "room", id, "error", err)
		observability.Warn(ctx, "room: push after leave failed", obsArgs...)
	}
}

// SCOPE:core — Shared session stream (session state + the shell's cross-page
// inbox count, no polling)
package auth

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"time"

	"github.com/calionauta/gogogo/config"
)

// defaultSessionRecheck is how often an OPEN session stream re-validates the
// cookie it was opened with. The banner only needs to notice DAYS-old expiry,
// not seconds, and every recheck is a session lookup; the stream is the
// delivery mechanism, not the check frequency. The same tick re-reads the
// shell count so a delivery from another process (the notifications bus is
// in-process) is picked up without a second wire.
const defaultSessionRecheck = 60 * time.Second

// SessionStreamExtras is the optional feed the session stream carries besides
// auth state: today the owner's unread inbox count. It is implemented at the
// composition root, so auth never imports a feature and a tree without the
// inbox simply leaves it nil.
type SessionStreamExtras interface {
	// Unread returns the owner's current unread count.
	Unread(ctx context.Context, owner string) (int, error)
	// Subscribe registers onChange for changes to owner's count and returns
	// the cancel func. The callback must not block.
	Subscribe(owner string, onChange func()) (cancel func())
}

// SetSessionStreamExtras wires the optional feed. Call once at boot, before
// the server starts serving (the stream reads it per connection).
func (a *Auth) SetSessionStreamExtras(x SessionStreamExtras) {
	a.streamExtras = x
}

// SessionStreamHandler serves the shared shell stream: one frame on connect
// with the current state, then only CHANGES.
//
// A page cannot ask the browser when its cookie expires and no server-side
// timer knows it either, so an open tab needs SOME periodic question. Here
// the answer rides a connection the tab already holds open, and the client
// never schedules a request. One VISIBLE tab of the browser holds it (Web
// Locks) and the rest hear the answer over a BroadcastChannel: an SSE stream
// holds an HTTP/1.1 connection, and a per-tab stream has been measured to
// fill the six-per-host budget at three tabs.
//
// The same connection carries the rail's unread count (an optional `unread`
// field on the frame), so a page whose own feature stream says nothing about
// the inbox still updates its badge when a reminder arrives or another tab
// reads one. One connection per browser, not one per signal.
func (a *Auth) SessionStreamHandler() http.Handler {
	return http.HandlerFunc(a.handleSessionStream)
}

// sessionSnapshot is the wire shape the banner and the rail badge consume. It
// mirrors collab.SessionEvent, which the notes and whiteboard streams send:
// one contract, two producers, so the client has a single `type:"session"`
// branch. Unread is a pointer so a page that cannot count (an anonymous
// request, or a tree with the inbox trimmed) omits the field instead of
// sending a misleading zero.
type sessionSnapshot struct {
	Type   string `json:"type"`
	Authed bool   `json:"authed"`
	Unread *int   `json:"unread,omitempty"`
}

// handleSessionStream streams the session state.
//
// An anonymous request gets ONE frame and the connection ends: there is no
// session to watch, and holding the socket would be a leak. A live stream
// that loses its session sends authed:false and closes, which tells the
// client to stop reconnecting (the client closes its EventSource on that
// frame — otherwise the browser's retry loop would be a poll with extra
// steps). A later re-login re-opens the stream on the next foreground, and
// the fresh request carries the new cookie.
func (a *Auth) handleSessionStream(w http.ResponseWriter, r *http.Request) {
	flusher, canFlush := w.(http.Flusher)
	if !canFlush {
		http.Error(w, "streaming unsupported", http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	w.WriteHeader(http.StatusOK)

	sess, serr := a.Session(r)
	authed := serr == nil && sess != nil && sess.User != nil
	owner := ""
	if authed {
		owner = sess.User.ID
	}

	// Subscribe BEFORE reading the first count: a delivery in the gap would
	// otherwise be missed until the 60s recheck. An early wake is buffered and
	// handled by the loop after the opening frame.
	wake := make(chan struct{}, 1)
	if authed && a.streamExtras != nil && owner != "" {
		cancel := a.streamExtras.Subscribe(owner, func() {
			select {
			case wake <- struct{}{}:
			default:
			}
		})
		defer cancel()
	}

	var unread *int
	last := -1
	if n, ok := a.unreadNow(r.Context(), owner); ok {
		unread = &n
		last = n
	}
	if !writeSessionSnapshot(w, authed, unread) {
		return
	}
	flusher.Flush()
	if !authed {
		return
	}

	recheck := a.sessionRecheck
	if recheck <= 0 {
		recheck = defaultSessionRecheck
	}
	heartbeat := time.NewTicker(config.DefaultSSEHeartbeatInterval)
	defer heartbeat.Stop()
	validate := time.NewTicker(recheck)
	defer validate.Stop()
	a.serveSession(w, flusher, r, owner, last, wake, heartbeat.C, validate.C)
}

// serveSession runs the stream's select loop: heartbeats keep the socket
// alive, the feed wakes it, and the recheck is both the expiry guard and the
// cross-process count safety net. It returns when the client goes away, the
// session dies, or a write fails.
func (a *Auth) serveSession(
	w http.ResponseWriter, f http.Flusher, r *http.Request,
	owner string, last int, wake <-chan struct{},
	heartbeat, validate <-chan time.Time,
) {
	for {
		select {
		case <-r.Context().Done():
			return
		case <-heartbeat:
			if !writeHeartbeat(w, f) {
				return
			}
		case <-wake:
			if !a.pushUnread(r.Context(), w, f, owner, &last) {
				return
			}
		case <-validate:
			if !a.sessionAlive(r) {
				if writeSessionSnapshot(w, false, nil) {
					f.Flush()
				}
				return
			}
			// The safety net for a count changed by another process: the
			// notifications bus is in-process, so a delivery handled by a
			// second instance reaches this browser on the next recheck.
			if !a.pushUnread(r.Context(), w, f, owner, &last) {
				return
			}
		}
	}
}

// sessionAlive answers the banner's question: a missing, unknown or expired
// cookie is false, never an error (this reports a state, it does not gate).
// Session resolution also deletes an expired row, so the revalidation has its
// side effect for free.
func (a *Auth) sessionAlive(r *http.Request) bool {
	sess, err := a.Session(r)
	return err == nil && sess != nil && sess.User != nil
}

// writeHeartbeat keeps intermediaries from closing an idle stream AND makes Go
// notice a dead socket: a parked writer never learns the peer is gone, so
// without this the handler leaks. false means the write failed.
func writeHeartbeat(w http.ResponseWriter, f http.Flusher) bool {
	if _, err := fmt.Fprint(w, ": heartbeat\n\n"); err != nil {
		return false
	}
	f.Flush()
	return true
}

// pushUnread re-reads the count and sends a frame when it changed. It returns
// false when the write failed (the client is gone). A read error is not fatal:
// the stream stays open and the next wake/recheck tries again.
func (a *Auth) pushUnread(
	ctx context.Context, w http.ResponseWriter, f http.Flusher, owner string, last *int,
) bool {
	n, ok := a.unreadNow(ctx, owner)
	if !ok || n == *last {
		return true
	}
	*last = n
	if !writeSessionSnapshot(w, true, &n) {
		return false
	}
	f.Flush()
	return true
}

// unreadNow reads the optional inbox count. ok=false when the tree has no
// inbox (no extras wired) or the owner is unknown; the stream then stays a
// pure session feed.
func (a *Auth) unreadNow(ctx context.Context, owner string) (int, bool) {
	if owner == "" || a.streamExtras == nil {
		return 0, false
	}
	n, err := a.streamExtras.Unread(ctx, owner)
	if err != nil {
		return 0, false
	}
	return n, true
}

// writeSessionSnapshot emits one unnamed SSE frame, the shape the notes and
// whiteboard streams use (hubs write `data: {...}` for JSON envelopes, and
// the banner parses JSON off `message`, not a named event). Unread rides the
// same frame: one type, one client branch, and the badge updates on whichever
// page the stream belongs to.
func writeSessionSnapshot(w http.ResponseWriter, authed bool, unread *int) bool {
	b, err := json.Marshal(sessionSnapshot{Type: "session", Authed: authed, Unread: unread})
	if err != nil {
		return false
	}
	_, err = fmt.Fprintf(w, "data: %s\n\n", b)
	return err == nil
}

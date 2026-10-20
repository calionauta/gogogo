// SCOPE:core — Shared session-expiry stream (banner signal, no polling)
package auth

import (
	"encoding/json"
	"fmt"
	"net/http"
	"time"

	"github.com/calionauta/gogogo/config"
)

// defaultSessionRecheck is how often an OPEN session stream re-validates the
// cookie it was opened with. The banner only needs to notice DAYS-old expiry,
// not seconds, and every recheck is a session lookup; the stream is the
// delivery mechanism, not the check frequency.
const defaultSessionRecheck = 60 * time.Second

// SessionStreamHandler serves the shared session banner as a live stream:
// one frame on connect with the current state, then only CHANGES.
//
// A page cannot ask the browser when its cookie expires and no server-side
// timer knows it either, so an open tab needs SOME periodic question. Here
// the answer rides a connection the tab already holds open, and the client
// never schedules a request. One VISIBLE tab of the browser holds it (Web
// Locks) and the rest hear the answer over a BroadcastChannel: an SSE stream
// holds an HTTP/1.1 connection, and a per-tab stream has been measured to
// fill the six-per-host budget at three tabs.
func (a *Auth) SessionStreamHandler() http.Handler {
	return http.HandlerFunc(a.handleSessionStream)
}

// sessionSnapshot is the wire shape the banner consumes. It mirrors
// collab.SessionEvent, which the notes and whiteboard streams send: one
// contract, two producers, so the client has a single `type:"session"`
// branch.
type sessionSnapshot struct {
	Type   string `json:"type"`
	Authed bool   `json:"authed"`
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

	authed := a.sessionAuthed(r)
	if !writeSessionSnapshot(w, authed) {
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

	for {
		select {
		case <-r.Context().Done():
			return
		case <-heartbeat.C:
			// Keeps intermediaries from closing an idle stream AND makes Go
			// notice a dead socket: a parked writer never learns the peer is
			// gone, so without this the handler leaks.
			if _, err := fmt.Fprint(w, ": heartbeat\n\n"); err != nil {
				return
			}
			flusher.Flush()
		case <-validate.C:
			if a.sessionAuthed(r) {
				continue
			}
			if writeSessionSnapshot(w, false) {
				flusher.Flush()
			}
			return
		}
	}
}

// sessionAuthed answers the banner's question: a missing, unknown or
// expired cookie is false, never an error (this reports a state, it does
// not gate). Session resolution also deletes an expired row, so the
// revalidation has its side effect for free.
func (a *Auth) sessionAuthed(r *http.Request) bool {
	_, err := a.Session(r)
	return err == nil
}

// writeSessionSnapshot emits one unnamed SSE frame, the shape the notes and
// whiteboard streams use (hubs write `data: {...}` for JSON envelopes, and
// the banner parses JSON off `message`, not a named event).
func writeSessionSnapshot(w http.ResponseWriter, authed bool) bool {
	b, err := json.Marshal(sessionSnapshot{Type: "session", Authed: authed})
	if err != nil {
		return false
	}
	_, err = fmt.Fprintf(w, "data: %s\n\n", b)
	return err == nil
}

// SCOPE:feature — native window live updates (JSON event stream)
package main

import (
	"bufio"
	"context"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/calionauta/gogogo/config"
	"github.com/calionauta/gogogo/internal/auth"
)

// eventsPath is the server's JSON record-event stream: one `data:` frame per
// todo mutation, scoped to the session. The web streams carry Datastar DOM
// patches, which a native renderer cannot consume; this endpoint exists for
// exactly that case (see features/todo/handlers/events.go).
const eventsPath = "/api/todos/events"

// eventBaseURL is where the local server is expected: the same HOST/PORT the
// web app binds. A wildcard bind is not a dial target, so it maps to loopback.
func eventBaseURL(cfg *config.Config) string {
	host := cfg.Host
	switch host {
	case "", "0.0.0.0", "::", "[::]":
		host = "127.0.0.1"
	}
	return "http://" + net.JoinHostPort(host, strconv.Itoa(cfg.Port))
}

// watchEvents holds one event stream open until ctx ends or the server
// closes it. It reports whether the connection was established (so the
// caller can reset its backoff) and every data frame triggers onEvent.
//
// A frame means "the list changed somewhere": the window re-lists from the
// database, the same rule the browser streams follow. No payload parsing, no
// DOM, no incremental state to get wrong.
func watchEvents(ctx context.Context, baseURL, token string, onEvent func()) (bool, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, baseURL+eventsPath, nil)
	if err != nil {
		return false, err
	}
	req.Header.Set("Accept", "text/event-stream")
	req.AddCookie(&http.Cookie{Name: auth.CookieName, Value: token})
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return false, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return false, fmt.Errorf("events: status %d", resp.StatusCode)
	}
	sc := bufio.NewScanner(resp.Body)
	sc.Buffer(make([]byte, 0, 4096), 1<<20)
	for sc.Scan() {
		// Comments (`: connected`, `: heartbeat`) keep the connection alive;
		// only a data frame is a change.
		if !strings.HasPrefix(sc.Text(), "data: ") {
			continue
		}
		onEvent()
	}
	return true, sc.Err()
}

// startEventWatcher keeps the stream connected for as long as the window is
// signed in. It is a *fallback-aware* path, not a replacement for the poll:
// the window may run standalone (no server listening), in which case the 2s
// local re-list is the only source. The retry backoff is exponential so a
// missing server costs one attempt every 30s, not a tight loop.
func startEventWatcher(ctx context.Context, st *windowState, baseURL string, onEvent func()) {
	backoff := time.Second
	const maxBackoff = 30 * time.Second
	for {
		if ctx.Err() != nil {
			return
		}
		token, signedIn := st.sessionToken()
		if !signedIn || token == "" {
			backoff = time.Second
			select {
			case <-ctx.Done():
				return
			case <-st.sessionCh:
			case <-time.After(backoff):
			}
			continue
		}

		connCtx, cancel := context.WithCancel(ctx)
		st.setEventsCancel(cancel)
		connected, err := watchEvents(connCtx, baseURL, token, onEvent)
		st.setEventsCancel(nil)
		cancel()
		if ctx.Err() != nil {
			return
		}
		if connected {
			// A real connection ended (server restart, deploy): reconnect
			// promptly. Only repeated connect failures back off.
			backoff = time.Second
			slog.Debug("gui: event stream closed", "error", err)
		} else {
			backoff *= 2
			if backoff > maxBackoff {
				backoff = maxBackoff
			}
			slog.Debug("gui: event stream unavailable", "error", err, "retry_in", backoff)
		}
		select {
		case <-ctx.Done():
			return
		case <-st.sessionCh:
			backoff = time.Second
		case <-time.After(backoff):
		}
	}
}

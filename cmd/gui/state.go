// SCOPE:feature — Window state + backend wiring for the gogpu/ui native window
package main

import (
	"context"
	"fmt"
	"sync"

	"github.com/google/uuid"

	"github.com/calionauta/gogogo/features/todo"
	"github.com/calionauta/gogogo/internal/auth"
)

// windowState is the single state object the native window mutates.
//
// THREADING CONTRACT (see also the task-4 review): gogpu/ui's App and
// Window are NOT thread-safe — all widget-tree operations must happen
// on the UI thread. This struct is shared by three parties: UI-thread
// event callbacks (button/textfield), the background poll goroutine
// (periodic refresh, see main.go), and headless tests. The rules:
//
//  1. Every method takes stateMu. Callbacks stay cheap; refresh() does
//     its SQLite I/O while holding the mutex but NEVER touches widgets.
//  2. View builders never read fields directly — they take a snapshot()
//     (a plain-data copy under the lock) and build from that, so a
//     concurrent refresh cannot tear a frame.
//  3. Rebuilds (SetRoot + RequestRedraw) are serialized by a separate
//     UI mutex owned by main.go. Lock order is always uiMu → stateMu,
//     never the reverse, so no deadlock. stateMu alone (without uiMu)
//     is held only for pure data operations like refresh().
//
// It is deliberately free of any UI-toolkit import: the same struct is
// exercised headless in state_test.go (no window, no GPU) and driven
// from gogpu/ui callbacks in views.go. Swapping the toolkit changes
// views.go only — this file and its tests stay untouched.
//
// Backend wiring is the flat stack's: guiStore (the sqlc queries the chi
// handlers use) + internal/auth (owned argon2id + sessions). No
// framework app handle, no database singleton, no cookie — the transport-free proof.
type windowState struct {
	mu sync.Mutex
	st guiStore
	a  *auth.Auth

	// auth phase
	signedIn bool
	ownerID  string
	token    string
	email    string
	password string
	authErr  string

	// list phase
	items []todo.Todo
	draft string
	err   string

	// sessionCh wakes the event watcher when the session appears or ends, so
	// it reconnects immediately instead of polling the state.
	sessionCh chan struct{}
	// eventsCancel stops the current event stream (set by the watcher,
	// invoked by signOut).
	eventsCancel func()
}

func newState(st guiStore, a *auth.Auth) *windowState {
	return &windowState{
		st: st, a: a, items: []todo.Todo{},
		sessionCh: make(chan struct{}, 1),
	}
}

// signalSession wakes the event watcher without blocking (one pending wake is
// enough: it re-reads the state on wake).
func (s *windowState) signalSession() {
	select {
	case s.sessionCh <- struct{}{}:
	default:
	}
}

// sessionToken reports whether a session is active and its token, for the
// event stream's cookie.
func (s *windowState) sessionToken() (string, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.token, s.signedIn
}

// setEventsCancel stores the current stream's cancel (nil when idle).
func (s *windowState) setEventsCancel(fn func()) {
	s.mu.Lock()
	s.eventsCancel = fn
	s.mu.Unlock()
}

// cancelEvents stops the active stream, if any. Safe to call repeatedly.
func (s *windowState) cancelEvents() {
	s.mu.Lock()
	fn := s.eventsCancel
	s.eventsCancel = nil
	s.mu.Unlock()
	if fn != nil {
		fn()
	}
}

// windowSnapshot is a consistent, lock-free copy of everything a frame
// needs. Slices are reallocated so the view can iterate without
// holding the mutex while laying out widgets.
type windowSnapshot struct {
	signedIn bool
	ownerID  string
	email    string
	authErr  string
	items    []todo.Todo
	draft    string
	err      string
}

func (s *windowState) snapshot() windowSnapshot {
	s.mu.Lock()
	defer s.mu.Unlock()
	items := make([]todo.Todo, len(s.items))
	copy(items, s.items)
	return windowSnapshot{
		signedIn: s.signedIn,
		ownerID:  s.ownerID,
		email:    s.email,
		authErr:  s.authErr,
		items:    items,
		draft:    s.draft,
		err:      s.err,
	}
}

func (s *windowState) setEmail(v string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.email = v
}

func (s *windowState) setPassword(v string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.password = v
}

func (s *windowState) setDraft(v string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.draft = v
}

// adoptSession installs a previously persisted session (token +
// ownerID) without asking for a password. Used at startup when a
// saved token still resolves. Falls back to refresh() so the first
// frame already shows data.
func (s *windowState) adoptSession(token, ownerID string) {
	s.mu.Lock()
	s.token = token
	s.ownerID = ownerID
	s.signedIn = true
	s.password = ""
	s.mu.Unlock()
	s.signalSession()
	s.refresh()
}

// signIn runs the transport-free login, then loads that owner's todos.
// Both steps take a plain ownerID string — no request, no cookie, no
// router. That is what internal/auth.Login + SessionByToken exist for.
// A password login that needs a second factor fails closed here: the
// native window has no TOTP field, so the user signs in on the web
// (VerifyTOTPLogin is an HTTP-surface concern today).
func (s *windowState) signIn() {
	s.mu.Lock()
	email, password := s.email, s.password
	s.mu.Unlock()

	ctx := context.Background()
	token, totpRequired, err := s.a.Login(ctx, email, password)
	if err != nil {
		s.mu.Lock()
		s.authErr = "wrong email or password"
		s.mu.Unlock()
		return
	}
	if totpRequired {
		s.mu.Lock()
		s.authErr = "two-factor required — sign in on the web"
		s.mu.Unlock()
		return
	}
	// Resolve the token back to an owner exactly as the HTTP path does:
	// a request carrying this token in its cookie yields this same id.
	// Keeping the token also means "remember me" is a storage concern
	// (see token.go), not a logic change.
	sess, err := s.a.SessionByToken(ctx, token)
	if err != nil {
		s.mu.Lock()
		s.authErr = "could not resolve session"
		s.mu.Unlock()
		return
	}
	s.mu.Lock()
	s.authErr = ""
	s.mu.Unlock()
	// One adoption path: it sets the session fields, clears the password,
	// wakes the event watcher and refreshes the list.
	s.adoptSession(token, sess.User.ID)
}

// refresh reloads the list from the store after every mutation, so the
// view never holds a locally-patched copy that could drift from the
// database. Safe to call from any goroutine: pure data, no widgets.
// This is also the fallback poll path: with a server reachable, the JSON
// event stream triggers the same refresh immediately; standalone (or with the
// server down) the tick keeps another frontend's writes visible within one
// interval.
func (s *windowState) refresh() {
	s.mu.Lock()
	ownerID := s.ownerID
	if ownerID == "" {
		s.mu.Unlock()
		return
	}
	st := s.st
	s.mu.Unlock()

	items, err := st.List(context.Background(), ownerID, "")
	s.mu.Lock()
	defer s.mu.Unlock()
	// A sign-out racing this refresh must win: if the owner changed
	// (or cleared) while the query was in flight, drop the result
	// rather than resurrecting the previous user's rows.
	if s.ownerID != ownerID {
		return
	}
	if err != nil {
		s.err = fmt.Sprintf("list: %v", err)
		return
	}
	s.items = items
	s.err = ""
}

func (s *windowState) add() {
	s.mu.Lock()
	if s.draft == "" || s.ownerID == "" {
		s.mu.Unlock()
		return
	}
	title, ownerID, st := s.draft, s.ownerID, s.st
	s.mu.Unlock()

	// idemKey is the offline-replay dedup token. This window is
	// online-only today (see main.go), but passing one keeps the call
	// identical to the web path, so adding an outbox later changes
	// nothing here.
	if _, err := st.Create(context.Background(), title, ownerID, uuid.NewString()); err != nil {
		s.mu.Lock()
		s.err = fmt.Sprintf("create: %v", err)
		s.mu.Unlock()
		return
	}
	// Clear the draft only on success — and only if it still holds the
	// submitted text: a keystroke that landed mid-create belongs to the
	// next todo, not to this one. A failed create keeps the text so the
	// user can retry instead of retyping.
	s.mu.Lock()
	if s.draft == title {
		s.draft = ""
	}
	s.mu.Unlock()
	s.refresh()
}

func (s *windowState) toggle(id string) {
	s.mu.Lock()
	if s.ownerID == "" {
		s.mu.Unlock()
		return
	}
	// Negation base is the LOCAL snapshot, not a fresh store read:
	// two frontends racing the same row is last-write-wins (pinned by
	// TestNativeCrossStateVisibilityAndLastWriteWins). The web tier
	// instead Get-then-Updates (fresh base) — same outcome class,
	// different read. Do not "fix" one side without the other.
	var completed bool
	for _, it := range s.items {
		if it.ID == id {
			completed = !it.Completed
		}
	}
	ownerID, st := s.ownerID, s.st
	s.mu.Unlock()

	if serr := st.SetCompleted(context.Background(), ownerID, id, completed); serr != nil {
		s.mu.Lock()
		s.err = fmt.Sprintf("toggle: %v", serr)
		s.mu.Unlock()
		return
	}
	s.refresh()
}

func (s *windowState) remove(id string) {
	s.mu.Lock()
	if s.ownerID == "" {
		s.mu.Unlock()
		return
	}
	ownerID, st := s.ownerID, s.st
	s.mu.Unlock()

	if err := st.Delete(context.Background(), ownerID, id); err != nil {
		s.mu.Lock()
		s.err = fmt.Sprintf("delete: %v", err)
		s.mu.Unlock()
		return
	}
	s.refresh()
}

func (s *windowState) signOut() {
	s.mu.Lock()
	fn := s.eventsCancel
	s.eventsCancel = nil
	s.signedIn = false
	s.ownerID = ""
	s.token = ""
	s.items = []todo.Todo{}
	s.mu.Unlock()
	// Stop the stream after unlocking (it wakes on a session signal below),
	// so a sign-out cannot leave a live feed for a session that ended.
	if fn != nil {
		fn()
	}
	s.signalSession()
}

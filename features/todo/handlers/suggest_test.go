// SCOPE:feature — HTTP todo suggest/retry contract tests (red-proof).
package handlers

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/calionauta/gogogo/internal/auth"
	"github.com/calionauta/gogogo/internal/db"
	dbgen "github.com/calionauta/gogogo/internal/db/sqlcgen"
	"github.com/calionauta/gogogo/internal/dbschema"
	"github.com/calionauta/gogogo/internal/llm"
	"github.com/calionauta/gogogo/internal/llm/fakeserver"
	"github.com/calionauta/gogogo/internal/queue"
)

type suggestFixture struct {
	t testing.TB
	r http.Handler
	h *Handler
}

func newSuggestFixture(tb testing.TB, withQueue, withLLM bool) *suggestFixture {
	tb.Helper()
	sqlDB, err := db.OpenAt(tb.TempDir() + "/todo-handlers-suggest.db")
	if err != nil {
		tb.Fatalf("db open: %v", err)
	}
	tb.Cleanup(func() { sqlDB.Close() })
	if merr := dbschema.Migrate(sqlDB); merr != nil {
		tb.Fatalf("db migrate: %v", merr)
	}
	a, err := auth.New(dbgen.New(sqlDB), false)
	if err != nil {
		tb.Fatalf("auth new: %v", err)
	}
	hub := queue.NewSSEHub()
	h := NewHandler(dbgen.New(sqlDB), a, hub, "daisyui")
	if withQueue {
		q, qerr := queue.NewAt(tb.TempDir() + "/todoqueue")
		if qerr != nil {
			tb.Fatalf("queue new: %v", qerr)
		}
		tb.Cleanup(q.Close)
		h.SetQueue(q)
		h.RegisterSuggestHandlers(q.Registry())
		h.RegisterRetryHandlers(q.Registry())
	}
	if withLLM {
		sim := llm.NewSimulatedWithDelay(time.Millisecond)
		tb.Cleanup(sim.Close)
		h.SetSimulatedLLMClient(sim)
	}
	r := http.NewServeMux()
	r.Handle("/api/auth/", a.Routes())
	r.Handle("/", h.Routes())
	return &suggestFixture{t: tb, r: r, h: h}
}

func (f *suggestFixture) signup(email string) *http.Cookie {
	f.t.Helper()
	body := `{"email":` + strconv.Quote(email) + `,"password":"Correct-horse-99!"}`
	req := httptest.NewRequestWithContext(context.Background(),
		http.MethodPost, "/api/auth/signup/credential", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	f.r.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		f.t.Fatalf("signup %s: status = %d (body: %s)", email, rec.Code, rec.Body.String())
	}
	for _, c := range rec.Result().Cookies() {
		if c.Name == auth.CookieName {
			return c
		}
	}
	f.t.Fatalf("signup %s: no %q cookie", email, auth.CookieName)
	return nil
}

func TestHandlersSuggestRequiresLLM(t *testing.T) {
	f := newSuggestFixture(t, true, false)
	cookie := f.signup("suggest-nollm@example.com")
	req := httptest.NewRequestWithContext(context.Background(),
		http.MethodPost, "/api/todos/suggest", strings.NewReader("partial=Plan"))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.AddCookie(cookie)
	rec := httptest.NewRecorder()
	f.r.ServeHTTP(rec, req)
	if rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("suggest without LLM: status = %d, want 503 (body: %s)", rec.Code, rec.Body.String())
	}
}

func TestHandlersRetryDemoRequiresQueue(t *testing.T) {
	f := newSuggestFixture(t, false, false)
	cookie := f.signup("retry-noq@example.com")
	req := httptest.NewRequestWithContext(context.Background(),
		http.MethodPost, "/api/todos/retry-demo", nil)
	req.AddCookie(cookie)
	rec := httptest.NewRecorder()
	f.r.ServeHTTP(rec, req)
	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("retry without queue: status = %d, want 500 (body: %s)", rec.Code, rec.Body.String())
	}
}

// TestHandlersRetryDemoAckOwnsNoSignals is the same ownership contract as the
// suggest ack, on the other endpoint that enqueues a job and then streams its
// result: /api/todos/retry-demo answers on the POST response while the retry
// worker pushes per-attempt frames on the tab's SSE stream. Writing
// `lastRetry` from the response let a slow ack overwrite the last attempt's
// text ("queued retry-demo job" landing after "attempt 3"). The response now
// means only "accepted".
func TestHandlersRetryDemoAckOwnsNoSignals(t *testing.T) {
	f := newSuggestFixture(t, true, false)
	cookie := f.signup("retry-ack@example.com")
	req := httptest.NewRequestWithContext(context.Background(),
		http.MethodPost, "/api/todos/retry-demo?clientID=tab-ack", nil)
	req.AddCookie(cookie)
	rec := httptest.NewRecorder()
	f.r.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("retry-demo: status = %d (body: %s)", rec.Code, rec.Body.String())
	}
	if ct := rec.Header().Get("Content-Type"); !strings.HasPrefix(ct, "text/event-stream") {
		t.Fatalf("retry-demo: content-type = %q, want SSE", ct)
	}
	for _, forbidden := range []string{"lastRetry", "demoStep", "lastRetryStatus"} {
		if strings.Contains(rec.Body.String(), forbidden) {
			t.Fatalf("retry-demo ack carries %q, which can clobber a worker frame (body: %s)",
				forbidden, rec.Body.String())
		}
	}
}

func TestHandlersSuggestSimulatedEnqueues(t *testing.T) {
	f := newSuggestFixture(t, true, true)
	cookie := f.signup("suggest-ok@example.com")
	req := httptest.NewRequestWithContext(context.Background(),
		http.MethodPost, "/api/todos/suggest-simulated", strings.NewReader("partial=Plan"))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.AddCookie(cookie)
	rec := httptest.NewRecorder()
	f.r.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("suggest-simulated: status = %d (body: %s)", rec.Code, rec.Body.String())
	}
	if ct := rec.Header().Get("Content-Type"); !strings.HasPrefix(ct, "text/event-stream") {
		t.Fatalf("suggest-simulated: content-type = %q, want SSE", ct)
	}
	// The ack must NOT touch the signals the worker's terminal patch owns.
	// The worker can stream its result before this ack reaches the browser
	// (the POST response travels back over the same slow link the stream
	// already occupies), so an ack that re-asserted aiPending=true re-stuck
	// the spinner on a completed run — aiStep=3, suggestions erased — and the
	// button said "Asking AI…" forever. Ordering is handled in the handler;
	// this pins the payload so a future ack cannot regress the terminal state.
	for _, forbidden := range []string{"aiPending", "aiStep", "suggestions", "suggestErr"} {
		if strings.Contains(rec.Body.String(), forbidden) {
			t.Fatalf("suggest ack carries %q, which can clobber a terminal patch (body: %s)",
				forbidden, rec.Body.String())
		}
	}
}

// TestHandlersSuggestAppendsAuthHint is the red-proof for the split-triple
// outage on the Suggest path: when the provider rejects the key (401), the
// error pushed over SSE must carry the key↔base mismatch hint. It drives
// handleSuggestJob against an in-process fake returning 401 (single
// attempt, no retry, milliseconds). Reverting suggest.go to err.Error()
// drops the hint and fails this test.
func TestHandlersSuggestAppendsAuthHint(t *testing.T) {
	srv := fakeserver.NewServer(
		fakeserver.WithStatusSequence(401, 401, 401),
		fakeserver.WithResponse("unreachable"),
	)
	t.Cleanup(srv.Close)
	t.Setenv("GOAI_BASE_URL", srv.URL)
	t.Setenv("GOAI_MODEL", "probe-model")

	f := newSuggestFixture(t, false, false)
	f.h.SetLLMClient(llm.New("test-key"))

	hub := queue.NewSSEHub()
	ch := make(chan []byte, 8)
	hub.Register("c1", "u1", ch)
	defer hub.Unregister("c1")

	payload, _ := json.Marshal(map[string]string{"partial": "Plan my week", "userId": "u1"})
	job := queue.Job{Type: signalJobSuggest, ClientID: "c1", Payload: payload}
	_ = f.h.handleSuggestJob(context.Background(), hub, job)

	select {
	case raw := <-ch:
		var envelope queue.Job
		if err := json.Unmarshal(raw, &envelope); err != nil {
			t.Fatalf("decode suggest envelope: %v", err)
		}
		if envelope.Type != jobTypeSuggestResult {
			t.Fatalf("envelope type = %q, want %q", envelope.Type, jobTypeSuggestResult)
		}
		var result map[string]any
		if err := json.Unmarshal(envelope.Payload, &result); err != nil {
			t.Fatalf("decode suggest result: %v", err)
		}
		got, _ := result[signalSuggestErr].(string)
		if !strings.Contains(got, "401") {
			t.Fatalf("provider failure lost from suggest error, got %q", got)
		}
		if !strings.Contains(got, "GOAI_API_KEY") || !strings.Contains(got, "GOAI_BASE_URL") {
			t.Fatalf("suggest auth failure missing the key↔base hint, got %q", got)
		}
	case <-time.After(10 * time.Second):
		t.Fatal("no suggest result arrived on hub channel")
	}
}

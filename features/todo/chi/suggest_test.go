// SCOPE:feature — Chi todo suggest/retry contract tests (red-proof).
package chi

import (
	"context"
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
	"github.com/calionauta/gogogo/internal/queue"
)

type suggestFixture struct {
	t testing.TB
	r http.Handler
	h *Handler
}

func newSuggestFixture(tb testing.TB, withQueue, withLLM bool) *suggestFixture {
	tb.Helper()
	sqlDB, err := db.OpenAt(tb.TempDir() + "/todo-chi-suggest.db")
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
		q, qerr := queue.NewAt(tb.TempDir() + "/chiq")
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
	r.Handle("/api/auth/", a.Handler())
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

func TestChiSuggestRequiresLLM(t *testing.T) {
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

func TestChiRetryDemoRequiresQueue(t *testing.T) {
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

func TestChiSuggestSimulatedEnqueues(t *testing.T) {
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
	if !strings.Contains(rec.Body.String(), "aiPending") {
		t.Fatalf("suggest-simulated ack missing aiPending (body: %s)", rec.Body.String())
	}
}

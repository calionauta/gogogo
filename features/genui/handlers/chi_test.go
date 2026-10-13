// SCOPE:feature — HTTP genui contract tests (red-proof).
package handlers

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/calionauta/gogogo/config"
	comp "github.com/calionauta/gogogo/features/genui/components"
	"github.com/calionauta/gogogo/internal/auth"
	"github.com/calionauta/gogogo/internal/db"
	dbgen "github.com/calionauta/gogogo/internal/db/sqlcgen"
	"github.com/calionauta/gogogo/internal/dbschema"
	"github.com/calionauta/gogogo/internal/queue"
)

// stubResponder answers with canned model output.
type stubResponder struct {
	raw string
	err error
}

func (s stubResponder) Respond(context.Context, string) (string, error) {
	return s.raw, s.err
}

func genuiFixture(t *testing.T, responder comp.Responder) http.Handler {
	t.Helper()
	sqlDB, err := db.OpenAt(t.TempDir() + "/genui.db")
	if err != nil {
		t.Fatalf("db open: %v", err)
	}
	t.Cleanup(func() { sqlDB.Close() })
	if merr := dbschema.Migrate(sqlDB); merr != nil {
		t.Fatalf("db migrate: %v", merr)
	}
	a, err := auth.New(dbgen.New(sqlDB), false)
	if err != nil {
		t.Fatalf("auth new: %v", err)
	}
	q, err := queue.NewAt(t.TempDir() + "/genuiqueue")
	if err != nil {
		t.Fatalf("queue: %v", err)
	}
	t.Cleanup(q.Close)
	h := NewHandler(config.Load(), a, q, queue.NewSSEHub(), responder)
	h.RegisterHandlers(q.Registry())
	q.StartWorkers()
	mux := http.NewServeMux()
	mux.Handle("/api/auth/", a.Handler())
	mux.Handle("/", h.Routes())
	return mux
}

func genuiSignup(t *testing.T, mux http.Handler, email string) *http.Cookie {
	t.Helper()
	body := `{"email":"` + email + `","password":"Correct-horse-99!"}`
	req := httptest.NewRequestWithContext(context.Background(),
		http.MethodPost, "/api/auth/signup/credential", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("signup: status = %d", rec.Code)
	}
	for _, c := range rec.Result().Cookies() {
		if c.Name == auth.CookieName && c.Value != "" {
			return c
		}
	}
	t.Fatal("no session cookie")
	return nil
}

func TestHandlersGenuiAuthGate(t *testing.T) {
	mux := genuiFixture(t, stubResponder{raw: "{}"})
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, httptest.NewRequestWithContext(context.Background(), http.MethodGet, "/genui", nil))
	if rec.Code != http.StatusSeeOther {
		t.Fatalf("anon /genui status = %d, want 303", rec.Code)
	}
}

func TestHandlersGenuiAskAndAnswer(t *testing.T) {
	mux := genuiFixture(t, stubResponder{
		raw: `{"components":[{"type":"text_note","props":{"text":"genui hi"}}]}`,
	})
	cookie := genuiSignup(t, mux, "genui@example.com")

	askRec := httptest.NewRecorder()
	askReq := httptest.NewRequestWithContext(context.Background(),
		http.MethodPost, "/api/genui/ask?clientID=genui-t1",
		strings.NewReader("prompt=show+something"))
	askReq.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	askReq.AddCookie(cookie)
	mux.ServeHTTP(askRec, askReq)
	if askRec.Code != http.StatusOK {
		t.Fatalf("ask status = %d (%s)", askRec.Code, askRec.Body.String())
	}
	if ct := askRec.Header().Get("Content-Type"); !strings.HasPrefix(ct, "text/event-stream") {
		t.Fatalf("ask content-type = %q, want SSE ack", ct)
	}

	// The worker renders through the queue: open the stream on a real
	// server and wait for the rendered answer event.
	srv := httptest.NewServer(mux)
	defer srv.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 8*time.Second)
	defer cancel()
	req, _ := http.NewRequestWithContext(ctx, http.MethodGet,
		srv.URL+"/api/genui/stream?clientID=genui-t1", nil)
	req.AddCookie(cookie)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("stream open: %v", err)
	}
	defer resp.Body.Close()
	buf := make([]byte, 16384)
	type readResult struct {
		n   int
		err error
	}
	got := ""
	deadline := time.Now().Add(7 * time.Second)
	for !strings.Contains(got, "genui hi") && time.Now().Before(deadline) {
		readCh := make(chan readResult, 1)
		go func() {
			n, err := resp.Body.Read(buf)
			readCh <- readResult{n: n, err: err}
		}()
		select {
		case res := <-readCh:
			got += string(buf[:res.n])
		case <-time.After(2 * time.Second):
		}
	}
	if !strings.Contains(got, "genui hi") {
		t.Fatalf("stream never rendered the answer (got %d bytes)", len(got))
	}
}

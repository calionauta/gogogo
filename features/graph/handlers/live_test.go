package handlers

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/calionauta/gogogo/config"
	"github.com/calionauta/gogogo/internal/auth"
	"github.com/calionauta/gogogo/internal/db"
	dbgen "github.com/calionauta/gogogo/internal/db/sqlcgen"
	"github.com/calionauta/gogogo/internal/dbschema"
	graphservice "github.com/calionauta/gogogo/internal/graph"
)

type fakeFetcher struct{ runs []graphservice.Run }

func (f fakeFetcher) ListRuns(context.Context, string, int) ([]graphservice.Run, error) {
	return f.runs, nil
}

type errFetcher struct{}

func (errFetcher) ListRuns(context.Context, string, int) ([]graphservice.Run, error) {
	return nil, context.DeadlineExceeded
}

const workflowDef = `{"name":"onboarding","steps":[` +
	`{"id":"greet","task":"onboarding-greet","depends_on":[]},` +
	`{"id":"await-first-todo","task":"onboarding-await-first-todo","depends_on":["greet"]}]}`

func graphFixture(t *testing.T, fetcher RunFetcher) http.Handler {
	t.Helper()
	sqlDB, err := db.OpenAt(t.TempDir() + "/graph.db")
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
	h := NewHandler(a, config.Load())
	h.SetWorkflowDefs([]string{workflowDef})
	if fetcher != nil {
		h.SetRunFetcher(fetcher)
	}
	mux := http.NewServeMux()
	mux.Handle("/api/auth/", a.Routes())
	mux.Handle("/", h.Routes())
	return mux
}

func graphSignup(t *testing.T, mux http.Handler) *http.Cookie {
	t.Helper()
	body := `{"email":"graph@example.com","password":"[LEAKGUARD_REDACTED] — Password Assignment"}`
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

func getJSON(t *testing.T, mux http.Handler, cookie *http.Cookie, path string) (int, map[string]any) {
	t.Helper()
	req := httptest.NewRequestWithContext(context.Background(), http.MethodGet, path, nil)
	req.AddCookie(cookie)
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	var body map[string]any
	_ = json.Unmarshal(rec.Body.Bytes(), &body)
	return rec.Code, body
}

func TestHandlerDagNatsShowsDefSteps(t *testing.T) {
	mux := graphFixture(t, nil)
	cookie := graphSignup(t, mux)
	code, body := getJSON(t, mux, cookie, "/api/graph/dagnats")
	if code != http.StatusOK {
		t.Fatalf("status = %d", code)
	}
	nodes, _ := body["nodes"].([]any)
	if len(nodes) == 0 {
		t.Fatalf("empty defs document: %v", body)
	}
}

func TestHandlerDagNatsOverlaysLiveRuns(t *testing.T) {
	mux := graphFixture(t, fakeFetcher{runs: []graphservice.Run{
		{
			ID: "r9", Workflow: "onboarding", Status: "running", CreatedAt: "2026-11-07T10:00:00Z",
			Steps: map[string]graphservice.StepStatus{"await-first-todo": {Status: "running"}},
		},
	}})
	cookie := graphSignup(t, mux)
	code, body := getJSON(t, mux, cookie, "/api/graph/dagnats?live=1")
	if code != http.StatusOK {
		t.Fatalf("status = %d", code)
	}
	nodes, _ := body["nodes"].([]any)
	for _, raw := range nodes {
		n, _ := raw.(map[string]any)
		if n["id"] == "onboarding:await-first-todo" {
			if n["status"] != "running" {
				t.Fatalf("await status = %v, want running", n["status"])
			}
			return
		}
	}
	t.Fatalf("await node missing in %v", body["nodes"])
}

func TestHandlerDagNatsWorkflowFilter(t *testing.T) {
	mux := graphFixture(t, nil)
	cookie := graphSignup(t, mux)
	code, body := getJSON(t, mux, cookie, "/api/graph/dagnats?workflow=nope")
	if code != http.StatusOK {
		t.Fatalf("status = %d", code)
	}
	if nodes, _ := body["nodes"].([]any); len(nodes) != 0 {
		t.Fatalf("filtered nodes = %v, want none", nodes)
	}
}

func TestHandlerDagNatsDegradesWhenEngineDown(t *testing.T) {
	mux := graphFixture(t, errFetcher{})
	cookie := graphSignup(t, mux)
	code, body := getJSON(t, mux, cookie, "/api/graph/dagnats?live=1")
	if code != http.StatusOK {
		t.Fatalf("status = %d, want 200 (degrade)", code)
	}
	if body["source"] != "dagnats:defs-only" {
		t.Fatalf("source = %v, want defs-only", body["source"])
	}
	if nodes, _ := body["nodes"].([]any); len(nodes) == 0 {
		t.Fatal("defs-only document must still carry the DAG")
	}
}

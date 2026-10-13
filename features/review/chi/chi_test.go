// SCOPE:feature — Review loop contract tests (red-proof).
package chi

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"testing"

	"github.com/firebase/genkit/go/ai"
	"github.com/firebase/genkit/go/genkit"
	jev "github.com/mattn/go-jev"
	"github.com/zendev-sh/goai/provider"
	"go.uber.org/goleak"

	"github.com/calionauta/gogogo/config"
	"github.com/calionauta/gogogo/internal/auth"
	"github.com/calionauta/gogogo/internal/db"
	dbgen "github.com/calionauta/gogogo/internal/db/sqlcgen"
	"github.com/calionauta/gogogo/internal/dbschema"
	"github.com/calionauta/gogogo/internal/llm"
	"github.com/calionauta/gogogo/internal/llm/fakeserver"
)

// errJudgeDown simulates an unreachable classifier backend.
var errJudgeDown = errors.New("review: judge backend unreachable")

// stubJudge answers the terminal classifier call with canned verdicts.
type stubJudge struct {
	verdict    string
	severity   string
	confidence float64
	err        error
	calls      int
}

func (s *stubJudge) Evaluate(_ context.Context, _, _ any) (*jev.Response, error) {
	s.calls++
	if s.err != nil {
		return nil, s.err
	}
	return &jev.Response{Answers: map[string]*jev.Answer{
		"verdict":  {Type: "choice", Choice: s.verdict, Confidence: s.confidence},
		"severity": {Type: "choice", Choice: s.severity, Confidence: 0.9},
	}}, nil
}

// echoModel registers a gogogo/llm action that never touches the network:
// maker prompts get a canned proposal, critic prompts get wantCritique.
// The APPROVE token distinguishes them (only critic prompts name it).
func echoModel(t *testing.T, g *genkit.Genkit, wantCritique string) ai.Model {
	t.Helper()
	return genkit.DefineModelAction(g, ReviewModelName,
		&ai.ModelOptions{Supports: &ai.ModelSupports{Multiturn: true}},
		func(_ context.Context, req *ai.ModelRequest, _ *struct{}, _ ai.ModelStreamCallback) (*ai.ModelResponse, error) {
			var b strings.Builder
			for _, m := range req.Messages {
				b.WriteString(m.Text())
			}
			text := "canned proposal"
			if strings.Contains(b.String(), "APPROVE") {
				text = wantCritique
			}
			return &ai.ModelResponse{
				Message:      ai.NewModelTextMessage(text),
				FinishReason: ai.FinishReasonStop,
			}, nil
		})
}

func reviewFixture(t *testing.T, critique string, judge *stubJudge) http.Handler {
	t.Helper()
	sqlDB, err := db.OpenAt(t.TempDir() + "/review.db")
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
	cfg := config.Load()
	g := NewGenkit()
	h := NewHandler(cfg, a, &Runner{
		G:         g,
		Model:     echoModel(t, g, critique),
		Judge:     judge,
		Threshold: 0.7,
	}, llm.New("test-key"))
	// Register the flow like BootChi does: the Dev UI/eval path shares
	// Runner.run, and registration itself must not panic on a second
	// definition within one process (each fixture owns its instance).
	h.RegisterFlows()
	mux := http.NewServeMux()
	mux.Handle("/api/auth/", a.Handler())
	mux.Handle("/", h.Routes())
	return mux
}

func reviewSignup(t *testing.T, mux http.Handler, email string) *http.Cookie {
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

func TestReviewRequiresAuth(t *testing.T) {
	mux := reviewFixture(t, "APPROVE", &stubJudge{verdict: DecisionApprove, confidence: 0.9})
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, httptest.NewRequestWithContext(context.Background(), http.MethodGet, "/review", nil))
	if rec.Code != http.StatusSeeOther {
		t.Fatalf("anon /review status = %d, want 303", rec.Code)
	}
	runRec := httptest.NewRecorder()
	runReq := httptest.NewRequestWithContext(context.Background(), http.MethodPost,
		"/api/review/run", strings.NewReader("tab=text&content=hello"))
	runReq.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	mux.ServeHTTP(runRec, runReq)
	if runRec.Code != http.StatusSeeOther {
		t.Fatalf("anon run status = %d, want 303", runRec.Code)
	}
}

func TestReviewRunApproves(t *testing.T) {
	mux := reviewFixture(t, "APPROVE", &stubJudge{verdict: DecisionApprove, confidence: 0.9})
	cookie := reviewSignup(t, mux, "review@example.com")
	rec := httptest.NewRecorder()
	req := httptest.NewRequestWithContext(context.Background(), http.MethodPost,
		"/api/review/run", strings.NewReader("tab=text&content=Some+draft+text"))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.AddCookie(cookie)
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("run status = %d (%s)", rec.Code, rec.Body.String())
	}
	body := rec.Body.String()
	if !strings.Contains(body, DecisionApprove) {
		t.Fatalf("run body lacks approve verdict:\n%s", body)
	}
}

func TestReviewLoopBounded(t *testing.T) {
	// Red-proof: a critic that always demands revision plus a revise
	// judge must terminate at MaxIterations, never loop. If the bound
	// regressed this test would hang instead of failing.
	judge := &stubJudge{verdict: DecisionRevise, severity: "low", confidence: 0.9}
	mux := reviewFixture(t, "REVISE: make it shorter", judge)
	cookie := reviewSignup(t, mux, "bound@example.com")
	rec := httptest.NewRecorder()
	req := httptest.NewRequestWithContext(context.Background(), http.MethodPost,
		"/api/review/run", strings.NewReader("tab=text&content=Some+draft+text"))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.AddCookie(cookie)
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("run status = %d (%s)", rec.Code, rec.Body.String())
	}
	if judge.calls != 1 {
		t.Fatalf("judge calls = %d, want exactly 1 (terminal judge)", judge.calls)
	}
	if !strings.Contains(rec.Body.String(), "3 rounds") {
		t.Fatalf("run body lacks bounded rounds marker:\n%s", rec.Body.String())
	}
}

func TestReviewJudgeUnavailableEscalates(t *testing.T) {
	mux := reviewFixture(t, "APPROVE", &stubJudge{err: errJudgeDown})
	cookie := reviewSignup(t, mux, "down@example.com")
	rec := httptest.NewRecorder()
	req := httptest.NewRequestWithContext(context.Background(), http.MethodPost,
		"/api/review/run", strings.NewReader("tab=text&content=Some+draft+text"))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.AddCookie(cookie)
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("run status = %d (%s)", rec.Code, rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), DecisionNeedsHuman) {
		t.Fatalf("run body lacks needs-human escalation:\n%s", rec.Body.String())
	}
}

func TestReviewLowConfidenceEscalates(t *testing.T) {
	mux := reviewFixture(t, "APPROVE",
		&stubJudge{verdict: DecisionApprove, confidence: 0.1})
	cookie := reviewSignup(t, mux, "lowconf@example.com")
	rec := httptest.NewRecorder()
	req := httptest.NewRequestWithContext(context.Background(), http.MethodPost,
		"/api/review/run", strings.NewReader("tab=text&content=Some+draft+text"))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.AddCookie(cookie)
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("run status = %d (%s)", rec.Code, rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), DecisionNeedsHuman) {
		t.Fatalf("low-confidence approve must escalate:\n%s", rec.Body.String())
	}
}

func TestReviewRunPersistsNothing(t *testing.T) {
	// Zero-artifact contract: the handler owns no queries, no queue, no
	// streams — construction takes none, so a run cannot persist. This
	// test pins the constructor surface: any future persistence dep
	// added to NewHandler breaks compilation here first.
	mux := reviewFixture(t, "APPROVE", &stubJudge{verdict: DecisionApprove, confidence: 0.9})
	cookie := reviewSignup(t, mux, "nowrite@example.com")
	rec := httptest.NewRecorder()
	req := httptest.NewRequestWithContext(context.Background(), http.MethodPost,
		"/api/review/run", strings.NewReader("tab=code&content=fix+me"))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.AddCookie(cookie)
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("run status = %d (%s)", rec.Code, rec.Body.String())
	}
}

func TestReviewRunWithoutLLMKeyFailsClosed(t *testing.T) {
	sqlDB, err := db.OpenAt(t.TempDir() + "/review-nokey.db")
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
	cfg := config.Load()
	g := NewGenkit()
	h := NewHandler(cfg, a, &Runner{
		G: g, Model: echoModel(t, g, "APPROVE"),
		Judge: &stubJudge{}, Threshold: 0.7,
	}, llm.New(""))
	mux := http.NewServeMux()
	mux.Handle("/api/auth/", a.Handler())
	mux.Handle("/", h.Routes())
	cookie := reviewSignup(t, mux, "nokey@example.com")
	rec := httptest.NewRecorder()
	req := httptest.NewRequestWithContext(context.Background(), http.MethodPost,
		"/api/review/run", strings.NewReader("tab=text&content=hi"))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.AddCookie(cookie)
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("keyless run status = %d, want 503", rec.Code)
	}
}

// TestReviewUnknownVerdictEscalates pins the closed-set default: a judge
// answering outside approve/revise must escalate, never auto-accept.
// Red-proof: change the default to DecisionRevise and this goes red.
func TestReviewUnknownVerdictEscalates(t *testing.T) {
	mux := reviewFixture(t, "APPROVE",
		&stubJudge{verdict: "maybe", severity: "low", confidence: 0.99})
	cookie := reviewSignup(t, mux, "weird@example.com")
	rec := httptest.NewRecorder()
	req := httptest.NewRequestWithContext(context.Background(), http.MethodPost,
		"/api/review/run", strings.NewReader("tab=text&content=Some+draft+text"))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.AddCookie(cookie)
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("run status = %d (%s)", rec.Code, rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), DecisionNeedsHuman) {
		t.Fatalf("unknown verdict must escalate:\n%s", rec.Body.String())
	}
}

// TestReviewHandlerOwnsNoPersistence is the zero-artifact tripwire: the
// Handler (and Runner) must never gain DB/queue/store/nats/credits
// fields. Persistence would arrive as a struct field first — this fails
// at that moment, before any write exists to catch. Red-proof: add
// `db *sql.DB` to Handler and watch it go red.
func TestReviewHandlerOwnsNoPersistence(t *testing.T) {
	banned := []string{
		"sql.DB", "queue.Queue", "sqlcgen", "dbschema", "nats.",
		"credits.", "collab.", "Queue", "Store", "Ledger",
	}
	check := func(name string, v any) {
		t.Helper()
		rt := reflect.TypeOf(v)
		if rt.Kind() == reflect.Pointer {
			rt = rt.Elem()
		}
		for f := range rt.Fields() {
			ft := f.Type.String()
			for _, b := range banned {
				if strings.Contains(ft, b) {
					t.Errorf("%s field %q has banned persistence type %q",
						name, f.Name, ft)
				}
			}
		}
	}
	check("Handler", Handler{})
	check("Runner", Runner{})
}

// TestReviewGenkitLeavesNoGoroutines guards the sync-by-design contract:
// no `go` statements exist in this package, so a full run must leave no
// goroutines behind. IgnoreCurrent covers the test runner; the SQLite
// connectionOpener is the driver's own (same ignores as the queue suite).
// NOTE: this package stays sequential on purpose — t.Parallel siblings
// (argon2 password hashing during signup) stay in flight while goleak
// snapshots, which reads as a leak. Speed sacrificed for a truthful
// assertion; see the skill's "audit first" rule.
func TestReviewGenkitLeavesNoGoroutines(t *testing.T) {
	defer goleak.VerifyNone(t,
		goleak.IgnoreCurrent(),
		goleak.IgnoreTopFunction("database/sql.(*DB).connectionOpener"),
		// genkit.Inits discarded signal.NotifyContext stop func parks one
		// signal_loop goroutine per Init call: third-party, process-lifetime
		// by design, inert for us (our flows run on the request ctx, never
		// the Init-derived one). Production Inits once; tests Init per
		// fixture. Anything OUR code spawns is still caught below.
		goleak.IgnoreTopFunction("os/signal.NotifyContext.func1"),
	)
	g := NewGenkit()
	model := echoModel(t, g, "APPROVE")
	r := &Runner{
		G: g, Model: model,
		Judge:     &stubJudge{verdict: DecisionApprove, confidence: 0.9},
		Threshold: 0.7,
	}
	if _, err := r.run(context.Background(),
		ReviewInput{Tab: TabText, Content: "hello"}, nil); err != nil {
		t.Fatalf("run: %v", err)
	}
}

// stubBiller counts authorize/settle calls through the llm choke point.
type stubBiller struct {
	authorizes int
	settles    int
}

func (s *stubBiller) Authorize(_ context.Context, _, _ string) (llm.Settle, error) {
	s.authorizes++
	return func(_ provider.Usage) { s.settles++ }, nil
}

// TestReviewModelBillsThroughChokePoint pins the single-meter invariant:
// a Genkit Generate through the gogogo/llm action must authorize once
// and settle once inside llm.Client — never zero (bypass), never two
// (a Genkit billing middleware beside the Biller). Red-proof: rewire
// the model fn around the client and authorizes stays 0 (red); wrap a
// second settle and settles reads 2 (red).
func TestReviewModelBillsThroughChokePoint(t *testing.T) {
	srv := fakeserver.NewServer(fakeserver.WithResponse("hello world"))
	defer srv.Close()
	t.Setenv("GOAI_BASE_URL", srv.URL)

	client := llm.New("test-key")
	meter := &stubBiller{}
	client.SetMeter(meter)

	g := NewGenkit()
	model := DefineReviewModel(g, client)
	resp, err := genkit.Generate(context.Background(), g,
		ai.WithModel(model), ai.WithPrompt("hi"))
	if err != nil {
		t.Fatalf("generate: %v", err)
	}
	if resp.Text() != "hello world" {
		t.Fatalf("text = %q, want fakeserver passthrough", resp.Text())
	}
	if meter.authorizes != 1 {
		t.Fatalf("authorizes = %d, want exactly 1", meter.authorizes)
	}
	if meter.settles != 1 {
		t.Fatalf("settles = %d, want exactly 1", meter.settles)
	}
}

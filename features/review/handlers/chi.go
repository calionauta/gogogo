// SCOPE:feature — Review loop (Genkit + goai + jev judge).
package handlers

import (
	"context"
	"net/http"
	"sync"
	"time"

	"github.com/go-chi/chi/v5"
	sdk "github.com/starfederation/datastar-go/datastar"

	"github.com/calionauta/gogogo/config"
	reviewcomponents "github.com/calionauta/gogogo/features/review/components"
	"github.com/calionauta/gogogo/internal/auth"
	dshelpers "github.com/calionauta/gogogo/internal/datastar"
	"github.com/calionauta/gogogo/internal/demo"
	"github.com/calionauta/gogogo/internal/llm"
	"github.com/calionauta/gogogo/internal/observability"
	"github.com/calionauta/gogogo/web/skins"
)

// Run signals (one spelling per signal, so painters and readers never drift).
const (
	signalReviewPending    = "review_pending"
	signalReviewError      = "review_error"
	signalReviewDecision   = "review_decision"
	signalReviewRationale  = "review_rationale"
	signalReviewConfidence = "review_confidence"
	signalReviewRounds     = "review_rounds"
	signalReviewProposal   = "review_proposal"
)

// Handler serves the review loop: page, synchronous run, streamed verdict.
// Synchronous by design: the run lives in the request scope (cancelled on
// disconnect), streams progress over ephemeral SSE, and persists nothing —
// no queue jobs, no DagNats runs, no dbgen writes. The billing ledger is
// the only write, via llm.Client's meter (generative calls) and the
// optional judge meter.
type Handler struct {
	cfg    *config.Config
	auth   *auth.Auth
	llm    *llm.Client
	runner *Runner
}

// NewHandler builds the review handler. Nil deps panic: wire errors
// surface at boot, not per request. g is the shared Genkit instance
// (one per process); model is the gogogo/llm action; judge is the
// jev-compatible backend (real *jev.Client or a test stub).
func NewHandler(cfg *config.Config, a *auth.Auth, r *Runner, llmClient *llm.Client) *Handler {
	if cfg == nil || a == nil || r == nil || llmClient == nil {
		panic("review chi: NewHandler with nil dependency")
	}
	return &Handler{cfg: cfg, auth: a, llm: llmClient, runner: r}
}

// Routes returns the chi router with the review page and API.
func (h *Handler) Routes() chi.Router {
	r := chi.NewRouter()
	r.Get("/review", h.handleIndex)
	r.Post("/api/review/run", h.handleRun)
	return r
}

// RegisterFlows registers the review flow on the shared Genkit registry
// for the Dev UI and eval harnesses. The HTTP path runs the same
// Runner.run directly (live SSE per phase); both share loop and bounds.
func (h *Handler) RegisterFlows() {
	h.runner.DefineReviewFlow()
}

// reviewEnabled is registration truth (wired) AND config truth (key set):
// the loop is real-LLM only, so an unconfigured server hides the UI.
func (h *Handler) reviewEnabled() bool {
	return h.llm != nil && h.llm.Configured()
}

func writeText(w http.ResponseWriter, code int, msg string) {
	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	w.WriteHeader(code)
	_, _ = w.Write([]byte(msg))
}

func (h *Handler) session(r *http.Request) *auth.ValidatedSession {
	sess, err := h.auth.Session(r)
	if err != nil {
		return nil
	}
	return sess
}

func redirectLogin(w http.ResponseWriter, r *http.Request) {
	http.Redirect(w, r, "/login", http.StatusSeeOther)
}

func (h *Handler) handleIndex(w http.ResponseWriter, r *http.Request) {
	sess := h.session(r)
	if sess == nil {
		redirectLogin(w, r)
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.WriteHeader(http.StatusOK)
	_ = reviewcomponents.ReviewIndex(sess.User.Email, sess.Role, h.cfg.BuildLabel, h.cfg.BuildCommit,
		reviewcomponents.Signals{ReviewEnabled: h.reviewEnabled()}, skins.Resolve(r, h.cfg.Skin),
		demo.On(r)).Render(r.Context(), w)
}

// handleRun executes one review loop synchronously and streams it:
// pending ack → per-phase progress → verdict patch. Cancelled when the
// client disconnects (request ctx); bounded by MaxRunDuration either way.
//
// context; disconnects surface as write errors, checked per call.
//
//nolint:contextcheck // SSE emission ends in datastar-go sinks, which take no
func (h *Handler) handleRun(w http.ResponseWriter, r *http.Request) {
	sess := h.session(r)
	if sess == nil {
		redirectLogin(w, r)
		return
	}
	if !h.reviewEnabled() {
		writeText(w, http.StatusServiceUnavailable, "LLM not configured: set GOAI_API_KEY")
		return
	}
	if err := r.ParseForm(); err != nil {
		writeText(w, http.StatusBadRequest, "invalid form")
		return
	}
	tab := r.FormValue("tab")
	content := r.FormValue("content")
	if content == "" {
		writeText(w, http.StatusBadRequest, "empty content")
		return
	}

	ctx, cancel := context.WithTimeout(r.Context(), MaxRunDuration)
	defer cancel()
	ctx = llm.WithUserID(ctx, sess.User.ID)

	// Heartbeat the SSE stream while the synchronous run works: a review
	// loop is several LLM calls with minutes of silence between the
	// pending ack and the verdict, and proxies kill idle streams —
	// leaving the button disabled forever with no error. Comments are
	// SSE-ignored so they only reset idle timers. All writes go through
	// the locked writer so a heartbeat can never interleave a patch.
	lw := &lockedWriter{w: w}
	sse := sdk.NewSSE(lw, r)
	stopBeat := heartbeat(ctx, lw)
	defer stopBeat()
	if err := dshelpers.MergeSignals(sse, map[string]any{
		signalReviewPending: true, signalReviewError: "",
	}); err != nil {
		obsArgs := append(observability.WithComponent("review", "feature"), "error", err)
		observability.Error(ctx, "review: run ack failed", obsArgs...)
		return
	}
	// Replace the previous verdict with the pending state BEFORE the loop
	// starts. Without it a second run left run #1's card on screen for the
	// whole (minutes-long) LLM window, so the tab looked finished while the
	// new run was still working — and if the new run then failed, the stale
	// card was all the user ever saw for that request.
	if err := dshelpers.RenderAndPatch(sse, reviewcomponents.ReviewPending(),
		sdk.WithSelector("#review-result")); err != nil {
		obsArgs := append(observability.WithComponent("review", "feature"), "error", err)
		observability.Error(ctx, "review: pending patch failed", obsArgs...)
		return
	}

	runner := *h.runner
	// The run executes synchronously in this goroutine: each phase chunk
	// forwards straight to SSE (no channels, no goroutines, nothing to
	// leak). The registered Genkit flow (same Runner.run) serves the Dev
	// UI and evals; the per-Generate model spans trace either path.
	spanCtx, endSpan := observability.StartSpan(ctx, "review.run")
	verdict, runErr := runner.run(spanCtx, ReviewInput{Tab: tab, Content: content},
		func(_ context.Context, phase string) error {
			return dshelpers.MergeSignals(sse, map[string]any{phaseSignal: phase})
		})
	endSpan()
	if runErr != nil {
		obsArgs := append(observability.WithComponent("review", "feature"), "error", runErr)
		observability.Error(ctx, "review: run failed", obsArgs...)
		_ = dshelpers.MergeSignals(sse, map[string]any{
			signalReviewPending: false, signalReviewError: "review failed: " + runErr.Error(),
		})
		// The error gets the region too: a signals-only failure left the
		// pending card (or the previous verdict) on screen with the message
		// parked in a one-line banner, which is how a failed run read as a
		// finished one.
		_ = dshelpers.RenderAndPatch(sse, reviewcomponents.ReviewError("review failed: "+runErr.Error()),
			sdk.WithSelector("#review-result"))
		return
	}
	_ = dshelpers.MergeSignals(sse, map[string]any{
		signalReviewPending:    false,
		signalReviewError:      "",
		signalReviewDecision:   verdict.Decision,
		signalReviewRationale:  verdict.Rationale,
		signalReviewConfidence: verdict.Confidence,
		signalReviewRounds:     verdict.Rounds,
		signalReviewProposal:   verdict.Proposal,
	})
	_ = dshelpers.RenderAndPatch(sse, reviewcomponents.ReviewResult(
		verdict.Decision, verdict.Rationale, verdict.Proposal, verdict.Critique,
		verdict.CriticVerdict, verdict.Confidence, verdict.Rounds,
		verdict.Decision == DecisionNeedsHuman),
		sdk.WithSelector("#review-result"))
}

// phaseSignal is the transient progress signal (writer/reviewer/judge).
// Seeded empty; each streamed chunk overwrites it.
const phaseSignal = "review_phase"

// heartbeatEvery is comfortably below common proxy idle timeouts
// (60s+) while adding negligible bytes to a minutes-long run. A var
// (not const) so tests observe a beat without sleeping 15s.
var heartbeatEvery = 15 * time.Second

// lockedWriter serializes every byte written to one SSE response.
// handleRun's synchronous phases and the heartbeat ticker write from
// different goroutines; without the mutex a comment could split a
// patch event mid-write and Datastar would drop both halves.
type lockedWriter struct {
	mu sync.Mutex
	w  http.ResponseWriter
}

func (l *lockedWriter) Header() http.Header { return l.w.Header() }

func (l *lockedWriter) Write(p []byte) (int, error) {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.w.Write(p)
}

func (l *lockedWriter) WriteHeader(statusCode int) {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.w.WriteHeader(statusCode)
}

func (l *lockedWriter) Flush() {
	l.mu.Lock()
	defer l.mu.Unlock()
	if f, ok := l.w.(http.Flusher); ok {
		f.Flush()
	}
}

// heartbeat writes SSE comments until ctx ends or stop runs. Comments
// carry no semantics — they only reset proxy idle timers. The loop
// selects on ctx.Done (never a bare ticker range) and stop is
// idempotent so defer + early exits share it safely.
func heartbeat(ctx context.Context, lw *lockedWriter) (stop func()) {
	t := time.NewTicker(heartbeatEvery)
	done := make(chan struct{})
	var once sync.Once
	stop = func() {
		once.Do(func() {
			t.Stop()
			close(done)
		})
	}
	go func() {
		defer stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-done:
				return
			case <-t.C:
				_, _ = lw.Write([]byte(": heartbeat\n\n"))
				lw.Flush()
			}
		}
	}()
	return stop
}

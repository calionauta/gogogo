// SCOPE:feature — Review loop (Genkit + goai + jev judge).
package chi

import (
	"context"
	"net/http"

	"github.com/go-chi/chi/v5"
	sdk "github.com/starfederation/datastar-go/datastar"

	"github.com/calionauta/gogogo/config"
	reviewcomponents "github.com/calionauta/gogogo/features/review/components"
	"github.com/calionauta/gogogo/internal/auth"
	dshelpers "github.com/calionauta/gogogo/internal/datastar"
	"github.com/calionauta/gogogo/internal/llm"
	"github.com/calionauta/gogogo/internal/observability"
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
	_ = reviewcomponents.ReviewIndex(sess.User.Email, h.cfg.BuildLabel, h.cfg.BuildCommit,
		reviewcomponents.Signals{ReviewEnabled: h.reviewEnabled()}).Render(r.Context(), w)
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

	sse := sdk.NewSSE(w, r)
	if err := dshelpers.MergeSignals(sse, map[string]any{
		signalReviewPending: true, signalReviewError: "",
	}); err != nil {
		obsArgs := append(observability.WithComponent("review", "feature"), "error", err)
		observability.Error(ctx, "review: run ack failed", obsArgs...)
		return
	}

	runner := *h.runner
	runner.UserID = sess.User.ID
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
		verdict.Decision, verdict.Rationale, verdict.Proposal,
		verdict.Confidence, verdict.Rounds, verdict.Decision == DecisionNeedsHuman),
		sdk.WithSelector("#review-result"))
}

// phaseSignal is the transient progress signal (writer/reviewer/judge).
// Seeded empty; each streamed chunk overwrites it.
const phaseSignal = "review_phase"

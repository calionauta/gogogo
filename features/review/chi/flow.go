// SCOPE:feature — Review loop (Genkit + goai + jev judge).
package chi

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/firebase/genkit/go/ai"
	"github.com/firebase/genkit/go/core"
	"github.com/firebase/genkit/go/genkit"
	jev "github.com/mattn/go-jev"

	"github.com/calionauta/gogogo/config"
	"github.com/calionauta/gogogo/internal/observability"
)

// Review tabs. Text is the default showcase (no code literacy needed,
// and classifier judges are most accurate on prose); code is the second
// tab for technical visitors. Same loop topology, different role prompts.
const (
	TabText = "text"
	TabCode = "code"
)

// Bounds: the loop always terminates. MaxIterations caps coder/reviewer
// rounds; the judge runs once, terminally. MaxRunDuration caps the whole
// run (the handler enforces it on the request ctx).
const (
	MaxIterations      = 3
	MaxRunDuration     = 90 * time.Second
	MaxContentRunes    = 6000
	MaxJudgeStateRunes = 6000
)

// Verdict decisions. approve/revise come from the judge; needs-human is
// the fail-soft outcome (judge unavailable, low confidence, or rounds
// exhausted) that surfaces the human approval gate in the UI.
const (
	DecisionApprove    = "approve"
	DecisionRevise     = "revise"
	DecisionNeedsHuman = "needs-human"
)

// ReviewInput is the typed flow input (Genkit infers the JSON schema).
type ReviewInput struct {
	Tab     string `json:"tab"`
	Content string `json:"content"`
}

// Verdict is the typed flow output.
type Verdict struct {
	Decision   string  `json:"decision"`
	Rationale  string  `json:"rationale"`
	Confidence float64 `json:"confidence"`
	Rounds     int     `json:"rounds"`
	Proposal   string  `json:"proposal"`
}

// Runner owns one review topology: the Genkit model action for the
// generative roles, the jev client for the terminal judge, and the
// confidence threshold below which verdicts escalate to a human.
type Runner struct {
	G         *genkit.Genkit
	Model     ai.Model
	Judge     JudgeClient
	Threshold float64
	// JudgeTimeout bounds the terminal judge call (attempts included).
	// Zero means a sane default; the client already enforces JEV_TIMEOUT
	// per attempt underneath.
	JudgeTimeout time.Duration
}

// DefineReviewFlow registers the streaming review flow: progress strings
// per phase, one Verdict at the end. The handler forwards each chunk to
// SSE; Run (non-streaming) works too and is what tests use.
func (r *Runner) DefineReviewFlow() *core.Flow[ReviewInput, Verdict, string] {
	return genkit.DefineStreamingFlow(r.G, "reviewRun",
		func(ctx context.Context, in ReviewInput, send core.StreamCallback[string]) (Verdict, error) {
			return r.run(ctx, in, send)
		})
}

// rolePrompts returns the (maker, critic) system prompts for a tab.
func rolePrompts(tab string) (maker, critic string) {
	if tab == TabCode {
		return "You are a programmer. Fix the bug(s) in the following diff/code. " +
				"Return ONLY the corrected code, no commentary.",
			"You are a strict code reviewer. Review the proposed fix against the " +
				"original. If it is correct and complete, reply with exactly: APPROVE. " +
				"Otherwise reply with: REVISE: <one paragraph of concrete required changes>."
	}
	return "You are a copy editor. Improve the following text for clarity and " +
			"tone. Return ONLY the improved text, no commentary.",
		"You are a strict editor. Review the draft below against the original. " +
			"If it is ready to publish, reply with exactly: APPROVE. Otherwise " +
			"reply with: REVISE: <one paragraph of concrete required changes>."
}

// truncateRunes caps s at n runes (jev demo endpoints enforce small
// contexts; custom endpoints accept more — the cap is a demo guardrail,
// documented in docs/review.md).
func truncateRunes(s string, n int) string {
	r := []rune(s)
	if len(r) <= n {
		return s
	}
	return string(r[:n])
}

// isApproval reports whether a critique is an approval: the contract
// asks critics to start approvals with exactly "APPROVE".
func isApproval(critique string) bool {
	return strings.HasPrefix(strings.ToUpper(strings.TrimSpace(critique)), "APPROVE")
}

func (r *Runner) generate(ctx context.Context, prompt string) (string, error) {
	resp, err := genkit.Generate(ctx, r.G, ai.WithModel(r.Model), ai.WithPrompt(prompt))
	if err != nil {
		return "", err
	}
	return resp.Text(), nil
}

// run executes coder→reviewer (bounded) → jev judge (terminal). Every
// phase emits a progress chunk; the judge step is the only jev call per
// run. Metering for the generative calls lives inside llm.Client; the
// judge call is recorded once below when a meter is wired.
func (r *Runner) run(ctx context.Context, in ReviewInput, send core.StreamCallback[string]) (Verdict, error) {
	tab := in.Tab
	if tab != TabCode {
		tab = TabText
	}
	content := strings.TrimSpace(in.Content)
	if content == "" {
		return Verdict{}, errors.New("review: empty content")
	}
	content = truncateRunes(content, MaxContentRunes)
	makerPrompt, criticPrompt := rolePrompts(tab)

	emit := func(s string) {
		if send == nil {
			return
		}
		_ = send(ctx, s)
	}

	proposal := content
	critique := ""
	rounds := 0
	makerInput := content
	for {
		emit("writer")
		made, err := r.generate(ctx, makerPrompt+"\n\n"+makerInput)
		if err != nil {
			return Verdict{}, err
		}
		proposal = strings.TrimSpace(made)
		emit("reviewer")
		crit, err := r.generate(ctx, criticPrompt+"\n\nOriginal:\n"+content+"\n\nDraft:\n"+proposal)
		if err != nil {
			return Verdict{}, err
		}
		critique = strings.TrimSpace(crit)
		rounds++
		if isApproval(critique) || rounds >= MaxIterations {
			break
		}
		makerInput = "A critic said:\n" + critique + "\n\nAddress every point. Original:\n" + content
		if ctx.Err() != nil {
			return Verdict{}, ctx.Err()
		}
	}

	emit("judge")
	state := truncateRunes("Original:\n"+content+"\n\nProposal:\n"+proposal+"\n\nCritique:\n"+critique, MaxJudgeStateRunes)
	verdictQuestion := "Should the proposal be accepted? Answer with exactly one of the allowed choices."
	severityQuestion := "If there are remaining issues, how severe are they?"
	ans, severity, confidence, judgeErr := r.judge(ctx, state, verdictQuestion, severityQuestion)
	if judgeErr != nil {
		obsArgs := append(observability.WithComponent("review", "feature"), "error", judgeErr)
		observability.Warn(ctx, "review: judge unavailable, escalating to human", obsArgs...)
		return Verdict{
			Decision:   DecisionNeedsHuman,
			Rationale:  "The automated judge is unavailable; a human should decide.",
			Confidence: 0, Rounds: rounds, Proposal: proposal,
		}, nil
	}

	// Closed set with a safe default: anything the judge returns outside
	// approve/revise escalates to a human. A default that auto-accepts
	// would bless every future verdict string sight unseen.
	decision := DecisionNeedsHuman
	rationale := "The judge returned an unrecognized verdict; a human should decide."
	switch {
	case ans == DecisionApprove && confidence >= r.threshold():
		decision = DecisionApprove
		rationale = "Approved by the automated judge (confidence " + formatConfidence(confidence) + ")."
	case ans == DecisionApprove:
		// Leans approve but below threshold: escalate, don't auto-accept.
		rationale = "The judge leans approve but confidence " + formatConfidence(confidence) +
			" is below threshold; a human should decide."
	case ans == DecisionRevise:
		decision = DecisionRevise
		rationale = "The judge requested revisions (severity " + severity + "): " + critique
	}
	return Verdict{
		Decision: decision, Rationale: rationale,
		Confidence: confidence, Rounds: rounds, Proposal: proposal,
	}, nil
}

// threshold returns the effective confidence threshold (config default;
// non-positive means unset → default, so tests need no config).
func (r *Runner) threshold() float64 {
	if r.Threshold <= 0 {
		return config.DefaultReviewConfidenceThreshold
	}
	return r.Threshold
}

func formatConfidence(c float64) string {
	return fmt.Sprintf("%.2f", c)
}

// judge runs the terminal classifier call: one verdict choice + one
// severity choice over the same state (shared prompt cache on the
// server). Returns (verdict, severity, confidence, error).
func (r *Runner) judge(ctx context.Context, state, verdictQ, severityQ string) (string, string, float64, error) {
	if r.Judge == nil {
		return "", "", 0, errors.New("review: no judge client wired")
	}
	timeout := r.JudgeTimeout
	if timeout <= 0 {
		timeout = 30 * time.Second
	}
	var cancel context.CancelFunc
	ctx, cancel = context.WithTimeout(ctx, timeout)
	defer cancel()
	resp, err := r.Judge.Evaluate(ctx, state, map[string]jev.Question{
		"verdict": {
			Type:         "choice",
			Instructions: verdictQ,
			Criteria:     map[string]any{DecisionApprove: nil, DecisionRevise: nil},
		},
		"severity": {
			Type:         "choice",
			Instructions: severityQ,
			Criteria:     map[string]any{"low": nil, "medium": nil, "high": nil},
		},
	})
	if err != nil {
		return "", "", 0, err
	}
	verdict, severity, confidence := "", "", 0.0
	if a := resp.Answers["verdict"]; a != nil {
		verdict, confidence = a.Choice, a.Confidence
	}
	if a := resp.Answers["severity"]; a != nil {
		severity = a.Choice
	}
	// Judge usage stays observable (slog below + streamed token counts),
	// not ledger-metered: the handler must not import the credits feature
	// (that would couple two trimmable units). Wire a meter only with a
	// consumer-side seam, never a second choke point beside llm.Biller.
	obsArgs := append(observability.WithComponent("review", "feature"),
		"verdict", verdict, "confidence", confidence,
		"input_tokens", resp.Usage.InputTokens)
	observability.Info(ctx, "review: judge answered", obsArgs...)
	return verdict, severity, confidence, nil
}

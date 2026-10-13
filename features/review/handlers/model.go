// SCOPE:feature — Review loop (Genkit + goai + jev judge).
package handlers

import (
	"context"
	"strings"

	"github.com/firebase/genkit/go/ai"
	"github.com/firebase/genkit/go/genkit"

	"github.com/calionauta/gogogo/internal/llm"
)

// ReviewModelName is the registry name of the gogogo model action: the
// single Genkit entry point to LLM generation. Flows reference the action
// object, never this string (string lookup would hit plugin resolution,
// which has no provider plugins registered by design).
const ReviewModelName = "gogogo/llm"

// NewGenkit builds the one Genkit instance the process shares (Genkit
// instances are goroutine-safe; define everything at startup, share
// across handlers). No plugins: provider SDKs would bypass the
// llm.Biller choke point, so generation reaches providers only through
// the model action below.
//
// Init registers a signal.NotifyContext it never stops: one per process,
// acceptable, documented here instead of hidden.
func NewGenkit() *genkit.Genkit {
	return genkit.Init(context.Background())
}

// reviewModelConfig is the model's typed config. Empty today: model,
// key and base URL ride process env through llm.Client (GOAI_*), so
// per-call config passthrough is a documented follow-up, not v1.
type reviewModelConfig struct{}

// DefineReviewModel registers the gogogo model action: Genkit's typed
// front door, llm.Client's choke point behind it. Metering stays inside
// ChatWithUsage (one authorize + one settle per call) — never add a
// Genkit billing middleware around it (double reserve in the ledger).
func DefineReviewModel(g *genkit.Genkit, client *llm.Client) *ai.ModelAction {
	return genkit.DefineModelAction(g, ReviewModelName,
		&ai.ModelOptions{
			Label:    "gogogo llm (internal/llm.Client)",
			Supports: &ai.ModelSupports{Multiturn: true, SystemRole: true, Tools: false},
		},
		reviewGenerate(client))
}

// reviewGenerate returns the model function with the client closed over.
// Named (not anonymous) so the long Genkit signature stays within the
// line-length gate and reads in one place.
func reviewGenerate(client *llm.Client) ai.ModelActionFunc[*reviewModelConfig] {
	return func(
		ctx context.Context, req *ai.ModelRequest,
		_ *reviewModelConfig, _ ai.ModelStreamCallback,
	) (*ai.ModelResponse, error) {
		// Non-streaming by design (v1): token-level streaming needs a
		// usage-returning stream accessor that llm.Client does not
		// expose yet. Progress streams at flow step boundaries
		// instead; the full text lands in one verdict patch.
		var b strings.Builder
		for _, m := range req.Messages {
			if t := m.Text(); t != "" {
				b.WriteString(t)
				b.WriteString("\n")
			}
		}
		text, usage, err := client.ChatWithUsage(ctx, strings.TrimSpace(b.String()))
		if err != nil {
			return nil, err
		}
		return &ai.ModelResponse{
			Message:      ai.NewModelTextMessage(text),
			FinishReason: ai.FinishReasonStop,
			Usage: &ai.GenerationUsage{
				InputTokens:         usage.InputTokens,
				OutputTokens:        usage.OutputTokens,
				TotalTokens:         usage.TotalTokens,
				ThoughtsTokens:      usage.ReasoningTokens,
				CachedContentTokens: usage.CacheReadTokens,
			},
		}, nil
	}
}

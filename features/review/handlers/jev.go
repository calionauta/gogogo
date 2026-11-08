// Review loop (Genkit + goai + jev judge).
//
// To remove: delete features/review/ + the wiring lines in
// internal/server/chi.go (BootChi construction + buildChiMux param/mounts)
// + the review capability/trim-unit entries + `go mod tidy` (drops
// github.com/firebase/genkit/go and github.com/mattn/go-jev when nothing
// else imports them).
//
// To point at another jev-compatible API: set JEV_API_URL + JEV_MODEL (+
// JEV_API_KEY when the server needs one). The contract is POST
// {state,model,questions} — THX-02, TypeSafe, self-hosted SimpleJev and
// tensai serve all speak it. No code changes needed. See docs/review.md.
package handlers

import (
	"context"
	"errors"
	"fmt"

	jev "github.com/mattn/go-jev"

	"github.com/calionauta/gogogo/config"
	"github.com/calionauta/gogogo/internal/observability"
)

// JudgeClient is the 1-method seam between the review flow and the
// jev-compatible judge backend. *jev.Client satisfies it; tests stub it.
type JudgeClient interface {
	Evaluate(ctx context.Context, state, questions any) (*jev.Response, error)
}

// Compile-time check that the mattn client satisfies the seam.
var _ JudgeClient = (*jev.Client)(nil)

// Compile-time check that the fallback chain satisfies the seam.
var _ JudgeClient = (*fallbackJudge)(nil)

// fallbackJudge is the primary→fallback chain: THX-02 first, SimpleJev
// when the primary fails for any reason (down, timed out, bad answer).
// A primary *answer* — even a low-confidence one — is never second
// guessed: it is the backend's decision, and the flow's confidence
// cascade decides from there.
type fallbackJudge struct {
	primary       JudgeClient
	fallback      JudgeClient
	primaryModel  string
	fallbackModel string
}

// Evaluate returns the primary answer, or the fallback's when the
// primary fails. When both fail, the error preserves both causes so
// the runner's needs-human path stays diagnosable.
func (f *fallbackJudge) Evaluate(ctx context.Context, state, questions any) (*jev.Response, error) {
	resp, err := f.primary.Evaluate(ctx, state, questions)
	if err == nil {
		return resp, nil
	}
	obsArgs := append(observability.WithComponent("review", "feature"),
		"primary_model", f.primaryModel,
		"fallback_model", f.fallbackModel,
		"primary_error", err)
	observability.Warn(ctx, "review: judge primary failed, trying fallback", obsArgs...)
	fresp, ferr := f.fallback.Evaluate(ctx, state, questions)
	if ferr != nil {
		return nil, fmt.Errorf("review: judge primary and fallback both failed: %w",
			errors.Join(err, ferr))
	}
	observability.Info(ctx, "review: judge answered via fallback", append(observability.WithComponent("review", "feature"),
		"fallback_model", f.fallbackModel)...)
	return fresp, nil
}

// NewJudgeClient builds the judge backend from config: THX-02 primary,
// SimpleJev fallback. A bare host expands via jev.Endpoint to
// /v1/systemone; a URL carrying a path (like the SimpleJev
// /v1/classifier fallback default) is used as-given. Empty keys send
// no Authorization header, which suits both keyless free tiers;
// operator keys live in the age-encrypted secrets file, never in git.
func NewJudgeClient(cfg *config.Config) JudgeClient {
	primary := jev.NewClient(
		jev.WithURL(jev.Endpoint(cfg.Review.APIURL)),
		jev.WithModel(cfg.Review.Model),
		jev.WithAPIKey(cfg.Review.APIKey),
		jev.WithTimeout(cfg.Review.Timeout),
	)
	fallback := jev.NewClient(
		jev.WithURL(jev.Endpoint(cfg.Review.FallbackAPIURL)),
		jev.WithModel(cfg.Review.FallbackModel),
		jev.WithAPIKey(cfg.Review.FallbackAPIKey),
		jev.WithTimeout(cfg.Review.Timeout),
	)
	return &fallbackJudge{
		primary:       primary,
		fallback:      fallback,
		primaryModel:  primary.Model(),
		fallbackModel: fallback.Model(),
	}
}

// SCOPE:feature — Review loop (Genkit + goai + jev judge).
//
// To remove: delete features/review/ + the wiring lines in
// internal/server/chi.go (BootChi construction + buildChiMux param/mounts)
// + the review capability/trim-unit entries + `go mod tidy` (drops
// github.com/firebase/genkit/go and github.com/mattn/go-jev when nothing
// else imports them).
//
// To point at another jev-compatible API: set JEV_API_URL + JEV_MODEL (+
// JEV_API_KEY when the server needs one). The contract is POST
// {state,model,questions} — TypeSafe, self-hosted SimpleJev and tensai
// serve all speak it. No code changes needed. See docs/review.md.
package handlers

import (
	"context"

	jev "github.com/mattn/go-jev"

	"github.com/calionauta/gogogo/config"
)

// JudgeClient is the 1-method seam between the review flow and the
// jev-compatible judge backend. *jev.Client satisfies it; tests stub it.
// The flow never names a provider — endpoint and model ride JEV_* env.
type JudgeClient interface {
	Evaluate(ctx context.Context, state, questions any) (*jev.Response, error)
}

// Compile-time check that the mattn client satisfies the seam.
var _ JudgeClient = (*jev.Client)(nil)

// NewJudgeClient builds the judge backend from config. A bare host expands
// via jev.Endpoint to /v1/systemone; a URL carrying a path (like the
// SimpleJev /v1/classifier default) is used as-given. Empty APIKey sends
// no Authorization header, which suits the keyless demo endpoint;
// operator keys live in the age-encrypted secrets file, never in git.
func NewJudgeClient(cfg *config.Config) *jev.Client {
	return jev.NewClient(
		jev.WithURL(jev.Endpoint(cfg.Review.APIURL)),
		jev.WithModel(cfg.Review.Model),
		jev.WithAPIKey(cfg.Review.APIKey),
		jev.WithTimeout(cfg.Review.Timeout),
	)
}

// The decision engine's wiring: build the client from config, or none.
//
// The endpoint chain is the app's ONE jev-like chain (THX-02 primary, the
// SimpleJev demo as the second leg), read from the shared JEV_* settings so the
// review judge, the Chat router and this engine all point at the same place.
package handlers

import (
	"context"
	"errors"
	"fmt"

	jev "github.com/mattn/go-jev"

	"github.com/calionauta/gogogo/config"
	"github.com/calionauta/gogogo/features/kiosk"
	"github.com/calionauta/gogogo/internal/observability"
)

// engineClient is the primary→fallback chain, the same shape the review judge
// and the Chat router use: THX-02 first, the demo classifier when the primary
// fails for any reason. A primary answer — even a shy one — is taken as-is; the
// engine's own confidence threshold is the arbiter.
type engineClient struct {
	primary  kioskEngineLeg
	fallback kioskEngineLeg
}

// kioskEngineLeg is the 1-method client shape the engine's seam declares.
type kioskEngineLeg = interface {
	Evaluate(ctx context.Context, state, questions any) (*jev.Response, error)
}

// Evaluate answers from the primary, or the fallback when the primary fails.
func (e *engineClient) Evaluate(ctx context.Context, state, questions any) (*jev.Response, error) {
	resp, err := e.primary.Evaluate(ctx, state, questions)
	if err == nil {
		return resp, nil
	}
	observability.Warn(ctx, "kiosk: engine primary failed, trying second leg",
		append(observability.WithComponent("kiosk", observability.LayerFeature), "error", err)...)
	resp, ferr := e.fallback.Evaluate(ctx, state, questions)
	if ferr != nil {
		return nil, fmt.Errorf("kiosk: engine primary and second leg both failed: %w",
			errors.Join(err, ferr))
	}
	observability.Info(ctx, "kiosk: engine answered via second leg",
		observability.WithComponent("kiosk", observability.LayerFeature)...)
	return resp, nil
}

// newClassifier builds the decision engine, or nil when the operator has not
// enabled it. A nil engine is the default and the compatibility guarantee: no
// client is constructed and no HTTP call is made, so the demo works keyless.
func newClassifier(cfg *config.Config) *kiosk.Classifier {
	if cfg == nil || !cfg.KioskClassifier.Enabled {
		return nil
	}
	primary := jev.NewClient(
		jev.WithURL(jev.Endpoint(cfg.Review.APIURL)),
		jev.WithModel(cfg.Review.Model),
		jev.WithAPIKey(cfg.Review.APIKey),
		jev.WithTimeout(cfg.Review.Timeout),
	)
	second := jev.NewClient(
		jev.WithURL(jev.Endpoint(cfg.Review.FallbackAPIURL)),
		jev.WithModel(cfg.Review.FallbackModel),
		jev.WithAPIKey(cfg.Review.FallbackAPIKey),
		jev.WithTimeout(cfg.Review.Timeout),
	)
	return kiosk.NewClassifier(
		&engineClient{primary: primary, fallback: second},
		cfg.KioskClassifier.ConfidenceThreshold,
	)
}

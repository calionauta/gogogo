// SCOPE:plugin — the jev-compatible Ask router's decision rules.
package handlers

import (
	"context"
	"errors"
	"testing"

	jev "github.com/mattn/go-jev"

	"github.com/calionauta/gogogo/config"
)

// stubJev answers the classifier call with one canned answer, never a
// network round-trip (the same seam features/review/handlers tests its
// judge through).
type stubJev struct {
	choice     string
	confidence float64
	answers    bool
	resp       *jev.Response
	err        error
}

func (s stubJev) Evaluate(_ context.Context, _, _ any) (*jev.Response, error) {
	if s.err != nil {
		return nil, s.err
	}
	if s.resp != nil {
		return s.resp, nil
	}
	resp := &jev.Response{Answers: map[string]*jev.Answer{}}
	if s.answers {
		resp.Answers[askRouterQuestion] = &jev.Answer{Type: "choice", Choice: s.choice, Confidence: s.confidence}
	}
	return resp, nil
}

// TestAskRouterChoose pins the four outcomes the fallback story rests on:
// a confident in-catalog pick is returned, and everything else is an
// empty pick (not an error) so Ask keeps offering the whole catalog.
func TestAskRouterChoose(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	for name, tc := range map[string]struct {
		stub    stubJev
		want    string
		wantErr bool
	}{
		"confident":       {stub: stubJev{answers: true, choice: "bar_chart", confidence: 0.9}, want: "bar_chart"},
		"at threshold":    {stub: stubJev{answers: true, choice: "data_table", confidence: 0.7}, want: "data_table"},
		"below threshold": {stub: stubJev{answers: true, choice: "bar_chart", confidence: 0.4}},
		"unknown label":   {stub: stubJev{answers: true, choice: "treemap", confidence: 0.99}},
		"chat only label": {stub: stubJev{answers: true, choice: "html_block", confidence: 0.99}},
		"empty choice":    {stub: stubJev{answers: true, choice: "", confidence: 0.99}},
		// A response that does not answer the question is malformed, not a
		// shy classifier: logged as a warning so a wrong endpoint or model
		// is visible instead of silently unused.
		"no answer at all":  {stub: stubJev{}, wantErr: true},
		"nil response body": {stub: stubJev{resp: &jev.Response{}}, wantErr: true},
	} {
		got, err := newJevRouter(tc.stub, 0.7).Choose(ctx, "plan my day")
		if tc.wantErr {
			if err == nil {
				t.Errorf("%s: Choose = %q, want an error", name, got)
			}
			continue
		}
		if err != nil {
			t.Fatalf("%s: Choose errored: %v", name, err)
		}
		if got != tc.want {
			t.Errorf("%s: Choose = %q, want %q", name, got, tc.want)
		}
	}
}

// TestAskRouterChooseReportsTransportErrors keeps the one real failure
// distinguishable from a fallback: AskPrompt logs it, and a router that is
// permanently misconfigured has to be visible.
func TestAskRouterChooseReportsTransportErrors(t *testing.T) {
	t.Parallel()
	cause := errors.New("connection refused")
	_, err := newJevRouter(stubJev{err: cause}, 0.7).Choose(context.Background(), "x")
	if err == nil {
		t.Fatal("transport failure returned no error, want one for the log")
	}
	if !errors.Is(err, cause) {
		t.Errorf("error %v does not wrap the cause, want it preserved for the log", err)
	}
}

// TestNewAskRouterIsOffByDefault is the opt-in guarantee: unless
// JEV_ROUTER_ENABLED is set, no router (and therefore no HTTP client and
// no changed prompt) exists. Red-proof: return a router unconditionally
// and this fails.
func TestNewAskRouterIsOffByDefault(t *testing.T) {
	t.Parallel()
	if got := NewAskRouter(&config.Config{}); got != nil {
		t.Errorf("NewAskRouter with AskRouter.Enabled=false = %v, want nil", got)
	}
	if got := NewAskRouter(nil); got != nil {
		t.Errorf("NewAskRouter(nil) = %v, want nil", got)
	}
	enabled := &config.Config{}
	enabled.AskRouter.Enabled = true
	enabled.AskRouter.ConfidenceThreshold = 0.8
	if got := NewAskRouter(enabled); got == nil {
		t.Error("NewAskRouter with AskRouter.Enabled=true = nil, want a router")
	}
}

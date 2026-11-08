// The jev-compatible Chat router's decision rules.
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
		resp.Answers[routerQuestion] = &jev.Answer{Type: "choice", Choice: s.choice, Confidence: s.confidence}
	}
	return resp, nil
}

// TestChatRouterChoose pins the four outcomes the fallback story rests on:
// a confident in-catalog pick is returned, and everything else is an
// empty pick (not an error) so Ask keeps offering the whole catalog.
func TestChatRouterChoose(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	for name, tc := range map[string]struct {
		stub    stubJev
		want    string
		wantErr bool
	}{
		"confident":             {stub: stubJev{answers: true, choice: "bar_chart", confidence: 0.9}, want: "bar_chart"},
		"at threshold":          {stub: stubJev{answers: true, choice: "data_table", confidence: 0.7}, want: "data_table"},
		"below threshold":       {stub: stubJev{answers: true, choice: "bar_chart", confidence: 0.4}},
		"unknown label":         {stub: stubJev{answers: true, choice: "treemap", confidence: 0.99}},
		"invented visual label": {stub: stubJev{answers: true, choice: "html_block", confidence: 0.99}, want: "html_block"},
		"empty choice":          {stub: stubJev{answers: true, choice: "", confidence: 0.99}},
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

// TestChatRouterChooseReportsTransportErrors keeps the one real failure
// distinguishable from a fallback: the prompt logs it, and a router that is
// permanently misconfigured has to be visible.
func TestChatRouterChooseReportsTransportErrors(t *testing.T) {
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

// countingJev wraps a stub with a call counter, so the fallback tests
// assert the fallback was — or was not — consulted.
type countingJev struct {
	inner stubJev
	calls int
}

func (c *countingJev) Evaluate(ctx context.Context, state, questions any) (*jev.Response, error) {
	c.calls++
	return c.inner.Evaluate(ctx, state, questions)
}

// TestChatRouterUsesClientFallback pins the same primary→fallback chain
// the review judge uses. Red-proof: return the primary error without
// consulting the fallback and the first case goes red.
func TestChatRouterUsesClientFallback(t *testing.T) {
	t.Parallel()
	ctx := context.Background()

	primaryDown := &countingJev{inner: stubJev{err: errors.New("connection refused")}}
	fallbackOK := &countingJev{inner: stubJev{answers: true, choice: "bar_chart", confidence: 0.9}}
	r := newJevRouter(&fallbackClient{primary: primaryDown, fallback: fallbackOK}, 0.7)
	got, err := r.Choose(ctx, "plan my day")
	if err != nil {
		t.Fatalf("fallback success returned error: %v", err)
	}
	if got != "bar_chart" {
		t.Errorf("Choose = %q, want fallback pick %q", got, "bar_chart")
	}
	if primaryDown.calls != 1 || fallbackOK.calls != 1 {
		t.Errorf("primary.calls=%d fallback.calls=%d, want 1 and 1", primaryDown.calls, fallbackOK.calls)
	}

	primaryOK := &countingJev{inner: stubJev{answers: true, choice: "data_table", confidence: 0.9}}
	fallbackIdle := &countingJev{inner: stubJev{answers: true, choice: "bar_chart", confidence: 0.9}}
	r = newJevRouter(&fallbackClient{primary: primaryOK, fallback: fallbackIdle}, 0.7)
	got, err = r.Choose(ctx, "plan my day")
	if err != nil {
		t.Fatalf("primary success returned error: %v", err)
	}
	if got != "data_table" {
		t.Errorf("Choose = %q, want primary pick %q", got, "data_table")
	}
	if fallbackIdle.calls != 0 {
		t.Errorf("fallback.calls = %d, want 0 when primary answers", fallbackIdle.calls)
	}
}

// TestNewRouterIsOffByDefault is the opt-in guarantee: unless
// JEV_ROUTER_ENABLED is set, no router (and therefore no HTTP client and
// no changed prompt) exists. Red-proof: return a router unconditionally
// and this fails.
func TestNewRouterIsOffByDefault(t *testing.T) {
	t.Parallel()
	if got := NewRouter(&config.Config{}); got != nil {
		t.Errorf("NewRouter with ChatRouter.Enabled=false = %v, want nil", got)
	}
	if got := NewRouter(nil); got != nil {
		t.Errorf("NewRouter(nil) = %v, want nil", got)
	}
	enabled := &config.Config{}
	enabled.ChatRouter.Enabled = true
	enabled.ChatRouter.ConfidenceThreshold = 0.8
	if got := NewRouter(enabled); got == nil {
		t.Error("NewRouter with ChatRouter.Enabled=true = nil, want a router")
	}
}

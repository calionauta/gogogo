package todo_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/url"
	"strings"
	"testing"
	"time"
)

// TestIntegration_SuggestSimulatedEnqueuesAndStreamsResult drives the
// full async path keyless: POST /api/todos/suggest-simulated enqueues a
// job, the worker runs it against the in-process fake LLM (which scripts
// 500 → 200 + delay), and the suggestions stream back over SSE. This
// exercises the queue + retry + SSE pipeline end to end without a token.
func TestIntegration_SuggestSimulatedEnqueuesAndStreamsResult(t *testing.T) {
	t.Parallel()
	base, _, _, _, cleanup := testFixtureSimulated(t)
	defer cleanup()

	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	clientID := "suggest-sim-client-" + time.Now().Format(clientIDSuffixFormat)
	stream := openSSEWithCtx(ctx, t, base, clientID)
	defer func() { _ = stream.Body.Close() }()

	// Give the SSE handler a beat to register the client before we POST.
	time.Sleep(100 * time.Millisecond)

	createURL := "http://127.0.0.1" + base[len("http://127.0.0.1"):] +
		"/api/todos/suggest-simulated?clientID=" + clientID
	resp, err := postForm(ctx, createURL, url.Values{titleField: {buyMilk}})
	if err != nil {
		t.Fatalf("suggest-simulated: %v", err)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("suggest-simulated status=%d", resp.StatusCode)
	}

	// Wait for the "suggestions" signal to arrive with 3 items. The
	// fake's 500→200 + delay means this lands after a retry + a slow
	// response, so we allow a generous timeout.
	//
	// The predicate waits for the TERMINAL state, not just the suggestions
	// signal: the signal lands first and the success toast follows a beat
	// later, so stopping at the signal alone would return a transcript that
	// still lacks the toast the assertions below require.
	//
	// It also must parse each event's signals payload rather than the raw
	// transcript — a transcript is `event:`/`data:` lines and each payload
	// is prefixed with `signals `, so unmarshalling the transcript (or a
	// raw payload) always fails and the predicate would never fire, silently
	// burning the whole timeout. See sseSignalPayloads.
	full := pumpSSEUntil(t, stream, 14*time.Second, func(s string) bool {
		return lastSuggestionsCount(s) == 3 && strings.Contains(s, "Got 3 suggestions")
	})
	if !strings.Contains(full, "\""+signalSuggestions+"\"") {
		t.Fatalf("suggest-simulated: suggestions never arrived: %s", tailString(full, 600))
	}
	if !strings.Contains(full, "Got 3 suggestions") {
		t.Fatalf("suggest-simulated: success toast missing: %s", tailString(full, 600))
	}
	// Regression guard: the AI Suggest run must flip its OWN stepper
	// signals (aiStep=3 + aiPending=false) when the result lands. These
	// are deliberately separate from the Queue + Retry demo's
	// techStep/techDone, so running one never lights the other's steps.
	if !strings.Contains(full, "\"aiStep\":3") {
		t.Fatalf("suggest-simulated: aiStep=3 UI signal missing: %s", tailString(full, 600))
	}
	if !strings.Contains(full, "\"aiPending\":false") {
		t.Fatalf("suggest-simulated: aiPending not cleared on success: %s", tailString(full, 600))
	}
}

// lastSuggestionsCount returns the length of the most recent `suggestions`
// signal in the transcript, or -1 when no such signal has arrived yet.
//
// Parsing the signals payload requires sseSignalPayloads: the transcript is
// `event:`/`data:` lines, and each payload additionally carries a `signals `
// prefix, so neither is JSON on its own.
func lastSuggestionsCount(transcript string) int {
	count := -1
	for _, payload := range sseSignalPayloads(transcript) {
		if !strings.Contains(payload, "\""+signalSuggestions+"\"") {
			continue
		}
		var patch map[string]json.RawMessage
		if err := json.Unmarshal([]byte(payload), &patch); err != nil {
			continue
		}
		raw, ok := patch[signalSuggestions]
		if !ok {
			continue
		}
		var sugg []string
		if err := json.Unmarshal(raw, &sugg); err != nil {
			continue
		}
		count = len(sugg)
	}
	return count
}

// TestIntegration_SuggestSimulatedShowsRetryFeedback asserts the worker's
// retry layer streams per-attempt feedback as the fake LLM returns 500 on
// the first call. This is the narration the user sees in the UI toasts.
func TestIntegration_SuggestSimulatedShowsRetryFeedback(t *testing.T) {
	t.Parallel()
	base, _, _, _, cleanup := testFixtureSimulated(t)
	defer cleanup()

	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	clientID := "suggest-retry-client-" + time.Now().Format(clientIDSuffixFormat)
	stream := openSSEWithCtx(ctx, t, base, clientID)
	defer func() { _ = stream.Body.Close() }()

	time.Sleep(100 * time.Millisecond)

	createURL := "http://127.0.0.1" + base[len("http://127.0.0.1"):] +
		"/api/todos/suggest-simulated?clientID=" + clientID
	resp, err := postForm(ctx, createURL, url.Values{titleField: {"write tests"}})
	if err != nil {
		t.Fatalf("suggest-simulated: %v", err)
	}
	defer func() { _ = resp.Body.Close() }()

	// The fake returns 500 on the first call, so we expect a "retry"
	// SSE event with attempt 1 before the eventual success.
	full := pumpSSEUntil(t, stream, 14*time.Second, func(s string) bool {
		return strings.Contains(s, "Got 3 suggestions")
	})
	if !strings.Contains(full, "suggest (simulated): attempt 1 failed") {
		t.Fatalf("suggest-simulated: retry feedback not seen: %s", tailString(full, 400))
	}
}

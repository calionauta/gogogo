// SCOPE:feature - Whiteboard tests + shared SSE stream helpers.
package whiteboard_test

import (
	"bufio"
	"context"
	"net/http"
	"slices"
	"strings"
	"testing"
	"time"
)

func openWBStream(t *testing.T, client *http.Client, baseURL, docID, clientID string) *wbStream {
	t.Helper()
	req, err := http.NewRequestWithContext(context.Background(), http.MethodGet,
		baseURL+"/api/whiteboard/"+docID+"/stream?clientID="+clientID, nil)
	if err != nil {
		t.Fatalf("wb stream req: %v", err)
	}
	req.Header.Set("Accept", "text/event-stream")
	resp, err := client.Do(req)
	if err != nil {
		t.Fatalf("wb stream connect: %v", err)
	}
	if resp.StatusCode != http.StatusOK {
		resp.Body.Close()
		t.Fatalf("wb stream status = %d", resp.StatusCode)
	}
	ctx, cancel := context.WithCancel(context.Background())
	s := &wbStream{resp: resp, cancel: cancel, events: make(chan string, 128)}
	go func() {
		defer close(s.events)
		sc := bufio.NewScanner(resp.Body)
		sc.Buffer(make([]byte, 0, 64*1024), 1024*1024)
		var buf strings.Builder
		for sc.Scan() {
			line := sc.Text()
			if line == "" {
				if buf.Len() > 0 {
					select {
					case s.events <- buf.String():
					case <-ctx.Done():
						return
					}
					buf.Reset()
				}
				continue
			}
			buf.WriteString(line)
			buf.WriteString("\n")
		}
	}()
	return s
}

type wbStream struct {
	resp   *http.Response
	cancel context.CancelFunc
	events chan string
}

func (s *wbStream) close() {
	s.cancel()
	_ = s.resp.Body.Close()
}

// wbWaitBudget bounds every "wait until the peer sees it" assertion. It is a
// DEADLINE, not a wait: a passing poll returns on the first matching event
// (single-digit milliseconds, because the SSE hub delivers in-process), and
// only a genuine failure ever pays the full budget. Before this, each such
// assertion slept a fixed 200-800ms window no matter how fast the event
// arrived, which is where most of this package's ~48s went.
const wbWaitBudget = 2 * time.Second

// drain returns the events that arrive within an explicit fixed window,
// discarding nothing. Reserve it for "assert an ABSENCE" checks, which
// genuinely must wait out the window because no event can short-circuit them.
func (s *wbStream) drain(window time.Duration) []string {
	var out []string
	timeout := time.After(window)
	for {
		select {
		case ev, ok := <-s.events:
			if !ok {
				return out
			}
			out = append(out, ev)
		case <-timeout:
			return out
		}
	}
}

// waitFor reads events until one satisfies match, returning every event seen
// (so a caller can assert on the whole transcript) or exhausting the budget.
//
// It polls in small slices so it can stop the moment the wanted event shows
// up. Exhausting the budget is NOT a failure: the caller keeps its original
// assertion, so this only ever makes a passing test fast — it cannot turn a
// failing assertion into a passing one.
func (s *wbStream) waitFor(budget time.Duration, match func(string) bool) []string {
	deadline := time.Now().Add(budget)
	var out []string
	for time.Now().Before(deadline) {
		out = append(out, s.drain(5*time.Millisecond)...)
		if slices.ContainsFunc(out, match) {
			return out
		}
	}
	return out
}

// waitForEvent is waitFor for a single wanted substring.
func (s *wbStream) waitForEvent(budget time.Duration, substr string) []string {
	return s.waitFor(budget, func(ev string) bool { return strings.Contains(ev, substr) })
}

// settleJoin lets the server register both streams before the test acts.
//
// It waits for the stream's OWN authoritative "count" event (broadcast to all
// clients on join) instead of a fixed sleep: once the connecting client has
// its count event, the hub has it registered and the peer's count event with
// it. Same guarantee, ~150-200ms cheaper per call.
func (s *wbStream) settleJoin(budget time.Duration) {
	_ = s.waitForEvent(budget, `"type":"count"`)
}

//go:build ruleguard

// Package gorules holds ruleguard rules loaded by golangci-lint's gocritic
// linter (settings.gocritic.settings.ruleguard.rules).
//
// These encode project-specific footguns that no off-the-shelf linter covers,
// so the guidance is enforced in CI instead of living only in prose a
// contributor (or an LLM) may never read.
//
// House rules for this file:
//   - One rule per footgun that was actually hit. Keep the set small; a rule
//     with false positives teaches people to ignore the linter.
//   - Never duplicate a stock linter. `perfsprint` already covers
//     fmt.Sprintf -> strconv, so no rule for it here.
//   - Patterns match EXPRESSIONS, not statements: `select { case ... }` does
//     not parse. Match the call and constrain with Where() when context
//     matters. The variadic group form is `$*name` (NOT `$$$name`, which does
//     not parse as a pattern), and it needs a name because Where() refers to it.
//   - A metavariable cannot be a selector receiver (`$x.Read($_)` does not
//     parse); use `$x.Read($_)` only where the receiver is already concrete in
//     the pattern, and keep the arg a single metavariable.
//
// The file needs `//go:build ruleguard` and the
// github.com/quasilyte/go-ruleguard/dsl module (go.mod `tool` directive).
package gorules

import "github.com/quasilyte/go-ruleguard/dsl"

// TimeAfterInSelect flags `<-time.After(...)` used directly as a select case.
//
// Every call allocates a fresh *time.Timer that is not reclaimed until it
// fires; inside a for/range loop, each iteration where another case wins leaks
// that timer until its full duration elapses — unbounded growth in a hot loop.
// Fixed by hand in internal/queue/workers.go.
//
// Matching the RECEIVE form (`<-time.After(...)`) rather than any `time.After`
// call is deliberate: `timeout := time.After(d)` hoisted above the loop is the
// correct one-shot idiom (see features/todo/handlers/onboarding.go) and must
// not be flagged. fakeserver uses the receive form in a one-request handler,
// not a loop.
func TimeAfterInSelect(m dsl.Matcher) {
	m.Match(`<-time.After($d)`).
		Where(!m.File().Name.Matches(`_test\.go|fakeserver`)).
		Report(`<-time.After(...) allocates a Timer per evaluation; inside a for-loop it leaks until it fires — hoist one time.NewTimer and Reset it (see internal/queue/workers.go)`)
}

// BlockingReadBehindDeadline flags a `Body.Read(...)` inside a `for` loop whose
// condition re-checks a wall-clock deadline, in test files.
//
// The loop LOOKS time-bounded but is not: Read blocks until the next event, so
// the deadline is only re-evaluated after that event. On a stream with nothing
// to say the wait is the SSE heartbeat (15s) regardless of the window asked
// for — a "6s" drain that takes 15.5s. It made every negative SSE assertion in
// features/todo cost a heartbeat, and it reads as an ordinary `for { Read }`,
// which is why it survived review.
//
// Matching the LOOP and checking its body for a Read keeps the rule local to
// the construct it describes, so it fires only when the deadline and the read
// are in the same loop rather than anywhere in the file.
//
// The fix is not a shorter timeout: move the Read into a goroutine and select
// on a timer, closing the body on expiry so the reader unblocks. See
// pumpSSEUntil in features/todo/sse_test.go, and
// TestPumpSSEUntil_HonorsDeadlineWhileReadParked which pins the guarantee.
func BlockingReadBehindDeadline(m dsl.Matcher) {
	m.Match(`for time.Now().Before($deadline) { $*body }`).
		Where(m["body"].Contains(`$x.Read($_)`)).
		Report(`Read blocks until the next event, so the surrounding "for time.Now().Before(deadline)" does not bound it — a silent stream waits one heartbeat past the deadline. Read in a goroutine and select on a timer (see pumpSSEUntil in features/todo/sse_test.go)`)
}

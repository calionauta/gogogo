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

// TickerLoopWithoutExit flags `for range $t.C` — a ticker loop with no exit.
//
// This is the shape that leaked a goroutine in a sibling project measured
// against this skill: a periodic-sync goroutine written as
//
//	go func() { t := time.NewTicker(...); defer t.Stop(); for range t.C { f.Sync() } }()
//
// `defer t.Stop()` stops the TICKER, which is the wrong resource: the
// goroutine is already parked in `for range t.C` and nothing can wake it. It
// is not a data race, so `-race` is silent; it is not a dead machine, so the
// tests pass; only a goroutine-leak checker or a shutdown assertion sees it.
// The correct shape selects on a cancellation source as well:
//
//	for { select { case <-ctx.Done(): return; case <-t.C: f() } }
//
// Scope: the repo has ZERO legitimate `for range t.C` (checked repo-wide — the
// only two mentions are in comments explaining this very trap), so this is a
// clean catch rather than a style preference. Helpers that DO want an
// unbounded loop take a `done <-chan struct{}` and select; every ticker loop
// in the tree (internal/nats/embedded.go waitForJetStream,
// internal/collab/presence.go, features/credits) already does.
//
// A test that wants to observe the leak deliberately keeps the bad shape and
// is exempt (see features/credits/lifecycle_test.go).
// TickerLoopWithoutExit flags `for range <ticker>.C` — a periodic loop with no
// exit.
//
// This is the shape that leaked a goroutine in a sibling project measured
// against this skill: a periodic-sync goroutine written as
//
//	go func() { t := time.NewTicker(...); defer t.Stop(); for range t.C { f.Sync() } }()
//
// `defer t.Stop()` stops the TICKER, which is the wrong resource: the
// goroutine is already parked in the range and nothing can wake it. It is not
// a data race, so `-race` is silent; it is not a deadlock, so the tests pass;
// only a goroutine-leak checker or a shutdown assertion sees it. The correct
// shape selects on a cancellation source as well:
//
//	for { select { case <-ctx.Done(): return; case <-t.C: f() } }
//
// Scope: the repo has ZERO legitimate `for range <x>.C` (verified repo-wide —
// the only occurrences are comments in this file), so every hit is the bug. A
// plain `make(chan int)` range has no `.C` and is not matched.
//
// A test that wants to observe the leak deliberately keeps the bad shape and
// is exempt (see features/credits/lifecycle_test.go).
//
// Split across two functions because ONE match arm cannot cover both receiver
// shapes, and they must be separate rule functions rather than two m.Match
// calls in one function (only the first is applied — verified against probe
// files). This matters: the single-arm version looked correct and silently
// missed half the bug.
func TickerLoopWithoutExit(m dsl.Matcher) {
	m.Match(`for range $x.C`).
		Where(!m.File().Name.Matches(`_test\.go`)).
		Report(`for range <ticker>.C has no exit: defer t.Stop() stops the ticker, not the goroutine parked on it, so nothing can wake it. Not a deadlock and not a race — which is why only a leak checker or a shutdown assertion sees it. Select on ctx.Done()/done as well (see internal/queue/workers.go waitOrStop, references/go-concurrency-deltas.md)`)
}

// TickerLoopWithoutExitCall is the second half of TickerLoopWithoutExit, for an
// inline call receiver (`for range time.NewTicker(d).C`) which the identifier
// arm does not reach. Separate function by necessity — see there.
func TickerLoopWithoutExitCall(m dsl.Matcher) {
	m.Match(`for range $f($*_).C`).
		Where(!m.File().Name.Matches(`_test\.go`)).
		Report(`for range <ticker>.C has no exit: defer t.Stop() stops the ticker, not the goroutine parked on it, so nothing can wake it. Not a deadlock and not a race — which is why only a leak checker or a shutdown assertion sees it. Select on ctx.Done()/done as well (see internal/queue/workers.go waitOrStop, references/go-concurrency-deltas.md)`)
}

// Deliberately NOT a rule here: `_ = x.Close()`.
//
// The bug that motivated it was a discarded HANDLE —
// `workersLocal := q.StartWorkers(); _ = workersLocal` — and ruleguard cannot
// express it: a two-statement sequence (`$r := $f(); _ = $r`) parses but never
// fires (verified). The nearest single-statement proxy, `_ = $recv.Close()`,
// was written and then REMOVED because it flags the idiomatic Go error-path
// cleanup — `if err != nil { _ = db.Close(); return nil, err }`, three real
// sites in features/credits/credits.go — so it would have taught people to
// ignore the linter, which is exactly what this file's house rules forbid.
//
// `_ = $r` is not a lint target but a REVIEW target, and the skill carries it
// as prose instead: go-concurrency-deltas.md, "store it on the owner".

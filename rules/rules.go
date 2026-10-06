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
//   - VERIFY THE RULE FIRES BEFORE TRUSTING IT. A rule that matches nothing is
//     indistinguishable from a rule that is broken, and a broken rule fails
//     open — CI stays green while the footgun ships. `rules_test.go` is the
//     guard; run it after editing this file.
//   - Prefer ONE arm that matches all the shapes over several arms: only the
//     first m.Match in a function is applied, and multiple arms can make
//     golangci-lint under-report (see TickerLoopWithoutExit).
//   - WHEN A RULE SEEMS NOT TO FIRE, SUSPECT THE FIXTURE FIRST. Three separate
//     false conclusions while writing this file came from the harness, not the
//     rule: a typecheck error anywhere in the package suppresses every rule
//     result; a missing `go-ruleguard/dsl` require makes loading fail with only
//     `used Run() with an empty rule set`; and a stale /tmp/golangci-lint.lock
//     makes the run refuse to start. All three look identical to "the rule never
//     fires" from the outside.

package gorules

import "github.com/quasilyte/go-ruleguard/dsl"

// TimeAfterInSelect flags `<-time.After(...)` inside a select, outside tests.
//
// `time.After` allocates a Timer per evaluation and the runtime cannot collect
// it until it fires, so a select inside a loop leaks timers for the lifetime of
// the loop's longest iteration. It is the idiomatic-looking version of a bug
// that only shows as memory growth under load.
//
// The one-shot idiom (a select reached once per call) is correct by
// construction and must not be flagged — see features/todo/handlers/onboarding.go.
// fakeserver uses the receive form in a one-request handler, not a loop.
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
// ONE ARM IS ENOUGH. `for range $x.C` matches both the identifier receiver
// (`for range t.C`) and the inline call receiver
// (`for range time.NewTicker(d).C`) — verified through golangci-lint and by
// running the ruleguard engine directly. A second arm for the call form is
// therefore redundant, so there is none.
//
// It was removed after measurement rather than kept "for safety", and the
// measurement is worth recording because it is counter-intuitive. With the
// fixture in rules_test.go (a call-chain loop FOLLOWED BY an identifier loop in
// one file), golangci-lint reports ONE diagnostic whether there are one or two
// arms. The reverse order (identifier first) reports 2 with two arms and 1 with
// one. So the second arm is not an improvement in either order — it adds a rule
// to maintain for no extra diagnostic, and one arrangement where it is not
// merely neutral. The enforcement is identical either way: a file with a leak
// fails the linter (exit 1) as long as ONE diagnostic is emitted, which the
// single arm guarantees.
//
// Scope: the repo has ZERO legitimate `for range <x>.C` (checked repo-wide — the
// only occurrences are comments in this file), so this is a clean catch rather
// than a style preference. Helpers that DO want an unbounded loop take a
// `done <-chan struct{}` and select; every ticker loop in the tree
// (internal/nats/embedded.go waitForJetStream, internal/collab/presence.go,
// features/credits) already does.
//
// A test that wants to observe the leak deliberately keeps the bad shape and
// is exempt (see features/credits/lifecycle_test.go).
func TickerLoopWithoutExit(m dsl.Matcher) {
	m.Match(`for range $x.C`).
		Where(!m.File().Name.Matches(`_test\.go`)).
		Report(`for range <ticker>.C has no exit: defer t.Stop() stops the ticker, not the goroutine parked on it, so nothing can wake it. Not a deadlock and not a race — which is why only a leak checker or a shutdown assertion sees it. Select on ctx.Done()/done as well (see internal/queue/workers.go waitOrStop, references/go-concurrency-deltas.md)`)
}

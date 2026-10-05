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
//     matters.
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

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
// construction and must not be flagged — see features/onboarding/onboarding.go
// (shutdownCtx).
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
// features/todo/handlers/stream_propagation_test.go, and features/todo/handlers/bridge_test.go
// for the shape (`go func(){ scanner.Scan(); close(seen) }()` + `select` on
// `time.After`).
func BlockingReadBehindDeadline(m dsl.Matcher) {
	m.Match(`for time.Now().Before($deadline) { $*body }`).
		Where(m["body"].Contains(`$x.Read($_)`)).
		Report(`Read blocks until the next event, so the surrounding "for time.Now().Before(deadline)" does not bound it — a silent stream waits one heartbeat past the deadline. Read in a goroutine and select on a timer (see features/todo/handlers/stream_propagation_test.go)`)
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

// Scope: the repo has ZERO legitimate sql.Open("sqlite") (checked
// repo-wide — the app database opens the tursogo "turso" driver in
// internal/db, and the ncruces "sqlite3" driver covers the test/gui
// paths). The modernc "sqlite" driver is not a dependency anymore, so
// opening that name would fail at runtime — and were it reintroduced
// beside the others it would create a second pool against the same
// files. This rule keeps OUR code from instantiating it.
func ModerncDriverOpen(m dsl.Matcher) {
	m.Match(`sql.Open("sqlite", $dsn)`).
		Report(`sql.Open("sqlite") instantiates the modernc driver, which is not a dependency of this repo — the app database opens "turso" (see internal/db) and test/gui paths open "sqlite3". A second pool against the same files is never what you want.`)
}

// LeakedErrText500JSON flags a 500 JSON body built with err.Error() — the
// internal text ships to the client. The flat stack has no Echo: handlers
// answer through local writeJSON/writeText helpers or net/http.
//
// The fix is the todo-slice shape (features/todo/handlers/chi.go fail()):
// log the error via internal/observability, return a static public string.
// Scope is DELIBERATELY 500-only. The 400 sites carry the reason the client
// needs to recover, and the audience is the authenticated caller who sent
// the bytes. Flagging those would push recovery reasons into logs the
// client never sees. The credits slice (features/credits/handlers/chi.go)
// keeps its 400s static for the same reason its 500 is static: gateway
// errors wrap driver detail ("credits: reserve: ...") the client cannot act
// on.
func LeakedErrText500JSON(m dsl.Matcher) {
	m.Match(`writeJSON($w, http.StatusInternalServerError, $*args)`).
		Where(m["args"].Contains(`$_.Error()`)).
		Report(`500 body built with err.Error() — the internal text ships to the client. Log it via observability and return a static public message (see features/todo/handlers/chi.go fail())`)
}

// LeakedErrText500Text is the same guard for the writeText spelling used by
// the todo, notes, whiteboard, chat, genui and onboarding slices. Only the
// FIRST m.Match in a rule function applies, so each spelling needs its own
// function. Same 500-only scope as above.
func LeakedErrText500Text(m dsl.Matcher) {
	m.Match(`writeText($w, http.StatusInternalServerError, $*args)`).
		Where(m["args"].Contains(`$_.Error()`)).
		Report(`500 body built with err.Error() — the internal text ships to the client. Log it via observability and return a static public message (see features/todo/handlers/chi.go fail())`)
}

// LeakedErrText500HTTP is the same guard for the net/http spelling used
// outside the slices (internal/auth). Same 500-only scope as above.
func LeakedErrText500HTTP(m dsl.Matcher) {
	m.Match(`http.Error($w, $msg, http.StatusInternalServerError)`).
		Where(m["msg"].Contains(`$_.Error()`)).
		Report(`500 body built with err.Error() — the internal text ships to the client. Log it server-side and return a static public message`)
}

// SlogBypassesObservability flags bare `slog.Error/Warn` inside HTTP slices
// (features/*/handlers, non-test): request-scoped failures must carry the request
// context (component tag + trace) via internal/observability, not a bare
// package-level log that drops both. Helpers take ctx as a parameter for
// exactly this reason (mustJSON, send, peerLeave, fail).
//
// Scope is DELIBERATELY Error/Warn-only in HTTP slices. Info/Debug stay free
// (boot chatter, progress), and packages outside HTTP slices (queue workers
// without a request,-ele installers, CLIs) keep plain slog — there is no
// request context to carry there, so forcing observability would only add
// `context.Background()` noise. PkgPath (not file name) scopes the rule so
// a same-named file elsewhere never matches.
func SlogBypassesObservability(m dsl.Matcher) {
	m.Match(`slog.$Level($*_)`).
		Where(m["Level"].Text.Matches(`^(Error|Warn)$`) &&
			m.File().PkgPath.Matches(`features/[a-z0-9]+/handlers`) &&
			!m.File().Name.Matches(`_test\.go`)).
		Report(`bare slog.$Level in an HTTP slice drops the request context — use observability.$Level with ctx (see internal/observability)`)
}

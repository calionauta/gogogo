## Linked issue

Closes #NNN (maintainer nod received: yes / no)

## Motivation / Approach / Impact / Open questions

<!-- Write this yourself — translation or copy-editing help is fine. -->

## Docs

<!-- The page this change updates, or "none — why". One line; the gates prove the mechanical half, this names the prose. -->
- Page(s): <the page this change updates, or "none — why">

## AI disclosure

<!-- Required, one line: which agent, what IT did, and what YOU did to verify it. -->
<!-- Name the agent that actually ran, never one by habit. The Pi harness is not -->
<!-- Claude, Codex or any other named client, and this template used to suggest -->
<!-- "Claude Code" in its example — which is how this repo got a PR crediting a -->
<!-- tool that never ran and attributing the author's review to the agent. -->
<!-- Shape: AI assistance: <agent + what it drafted>; I <what I checked>. -->
<!-- Keep the subjects straight: the agent did the drafting, YOU reviewed, -->
<!-- rejected, corrected and verified. Never write "I" for something the agent -->
<!-- did, and never credit the agent with the judgement call that shipped it. -->
<!-- No AI trailers in commits, release notes, or CHANGELOG.md — keep provenance here. -->

## Verification

- `make signoff` result:
- Red-to-green evidence (failing test before, passing after):

## Checklist

- [ ] One concern per PR, no drive-by refactors or unrelated files
- [ ] A new removable unit has its row in `internal/capabilities` (kind, owned dirs/files, `Register` when it mounts itself) — the registry is the only place the removal class lives
- [ ] `make templ && make css` after `.templ` edits; no hand-edited `_templ.go`
- [ ] No real LLM in tests; ephemeral ports (`127.0.0.1:0`, NATS `-1`); `GOGOGO_NO_BROWSER=1` where applicable
- [ ] New dependency or native code: passed `docs/dependency-principles.md`, and the one-line trade-off record (chosen / rejected / cost / revisit-if) is in this body
- [ ] No dependency raised the native floor (same C compiler, same musl image, same cross-compile, no new runtime library); any human-gate item got an explicit sign-off
- [ ] I understand every line and own its licensing; I can defend it in review
- [ ] Security invariants preserved: session check + RBAC row for every new route; owner-scoped record access; no request-derived URL/command/file path; inline scripts carry the CSP nonce
- [ ] Negative tests added for new authorization, CSRF, signup/session, or rate-limit behavior
- [ ] CI green (PRs with red CI are not reviewed until green)

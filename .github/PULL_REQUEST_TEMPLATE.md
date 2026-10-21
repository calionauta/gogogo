## Linked issue

Closes #NNN (maintainer nod received: yes / no)

## Motivation / Approach / Impact / Open questions

<!-- Write this yourself — translation or copy-editing help is fine. -->

## AI disclosure

<!-- Required, one line: which tool, what it did, what you verified. -->
<!-- e.g. AI assistance: Claude Code drafted the handler and tests; I reviewed, trimmed, and verified them. -->
<!-- No AI trailers in commits, release notes, or CHANGELOG.md — keep provenance here. -->

## Verification

- `make signoff` result:
- Red-to-green evidence (failing test before, passing after):

## Checklist

- [ ] One concern per PR, no drive-by refactors or unrelated files
- [ ] `// SCOPE:<core|plugin|feature>` on new non-test, non-generated `.go` files (`make check-scope`)
- [ ] `make templ && make css` after `.templ` edits; no hand-edited `_templ.go`
- [ ] No real LLM in tests; ephemeral ports (`127.0.0.1:0`, NATS `-1`); `GOGOGO_NO_BROWSER=1` where applicable
- [ ] New dependency or native code: passed `docs/dependency-doctrine.md`, and the one-line trade-off record (chosen / rejected / cost / revisit-if) is in this body
- [ ] No dependency raised the native floor (same C compiler, same musl image, same cross-compile, no new runtime library); any human-gate item got an explicit sign-off
- [ ] I understand every line and own its licensing; I can defend it in review
- [ ] CI green (PRs with red CI are not reviewed until green)

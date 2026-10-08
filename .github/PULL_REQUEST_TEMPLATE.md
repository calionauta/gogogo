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
- [ ] `// SCOPE:layer=…,removal=…` on new non-test, non-generated `.go` files (`make check-scope`)
- [ ] `make templ && make css` after `.templ` edits; no hand-edited `_templ.go`
- [ ] No real LLM in tests; ephemeral ports (`127.0.0.1:0`, NATS `-1`); `GOGOGO_NO_BROWSER=1` where applicable
- [ ] No new dependency or native code without prior discussion
- [ ] I understand every line and own its licensing; I can defend it in review
- [ ] CI green (PRs with red CI are not reviewed until green)

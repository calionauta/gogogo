## Two failure shapes no linter catches

Both were shipped and neither `golangci-lint` nor `go vet` noticed, because
both produce valid Go that does what the code says.

**1. A checked error whose cause you replace with a literal.**

```go
raw, err := h.responder.Respond(ctx, p.Prompt)
if err != nil {
    h.sendResult(target, id, nil, "Ask failed: model unavailable")  // err discarded
    return nil
}
```

The branch is *handled*, so nothing complains — but the responder's own
message ("no LLM configured (GOAI_API_KEY or SIMULATE_LLM=true)", or the
provider's) was the only actionable part and it was thrown away. A missing key
and a dead provider became indistinguishable to the user.

Rule: **the cause reaches the caller or the log.** If you deliberately replace
it, a test must assert the replacement still separates the causes you claim it
does. `nilerr` catches the adjacent mistake (returning nil after checking
err); it cannot catch this one.

**2. A closed set with a wrong `default`.**

```go
isSuggest := p.Operation == jobSuggest || p.Operation == jobSuggestSimulated
if isSuggest { return aiFields }
return demoFields          // ← everything that is not suggest is the retry demo
```

`genui_ask` and `chat_ask` are neither, so they lit the Queue + Retry demo
the user never started — and emitted `"genui_ask: completed"` beside the
worker's own `"Ask failed"` toast, reading as the opposite of what happened.

Rule: **when a value selects a UI target, ask which values own one instead of
defaulting the rest to a target.** A `default` that performs an action is a
bet that every future value wants it. `exhaustive` would help here only if the
job types were typed enums rather than plain strings — they are not, so this
is a review item, and the contract test that pins both directions (the three
owners still light, everything else emits nothing) is what makes it stick.

The trim gate was the third shape and the one with a mechanical answer: a strip
rule whose marker no longer matches must be a **failure**, never a warning.
Trim itself is retired (scaffold keeps everything), so the failure class now
only bites the port bundle: a drifted strip marker is caught by the registry
conformance tests and the export producer's own list.


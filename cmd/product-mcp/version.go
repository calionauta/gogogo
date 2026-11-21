package main

// version is what `product_describe` reports, so an agent comparing two
// deployments can tell them apart. No target in THIS repo stamps it: `make
// build`, `install.sh` and `.goreleaser.yaml` ship the evolution pair, and each
// product compiles its own MCP (see docs/mcp.md, "Which one do I install?").
//
// A product that wants a real number stamps this one — the same shape as the
// evolution binaries, one variable, no second release tag:
//
//	go build -ldflags "-X main.version=1.4.2" ./cmd/product-mcp
//
// Unstamped, it reports "dev". That is honest rather than fake: a made-up
// number would make two different builds look identical to the agent that
// compared them, which is worse than an obvious placeholder.
var version = "dev"

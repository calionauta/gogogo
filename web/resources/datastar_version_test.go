// SCOPE:core - The vendored Datastar core bundle's version is pinned by test.
package resources

import (
	"io/fs"
	"strings"
	"testing"
)

// pinnedDatastarCore is the JS core version this repo vendors. Bump it in the
// same commit that replaces the file (see the upgrade notes below).
const pinnedDatastarCore = "v1.0.4"

// TestDatastarCoreVersionIsPinned exists because the JS core and the Go SDK
// version INDEPENDENTLY, and conflating them is a measured mistake: AGENTS.md
// reads "Datastar v1.2.2", which is `datastar-go` (the server SDK) — the
// browser actually runs the bundle below, and for a while the docs, this
// skill and an agent summary all claimed the browser half was on 1.2.2 too.
// Reasoning about browser behavior (which attributes exist, whether scoping
// exists) from the SDK version is wrong by definition, and nothing failed to
// say so.
//
// So the bundle's own banner is the source of truth and this test is the
// gate: replacing the file without the version constant (or the reverse) is
// a red build rather than a stale doc.
//
// UPGRADING the bundle — what to check, learned the hard way on 1.0.3 → 1.0.4:
//  1. fetch the official bundles/datastar.js for the tag, never a CDN build
//     of an untagged commit;
//  2. diff the plugin-name sets (`name:"<x>",…` in the minified file). An
//     equal set means no attribute disappeared — the 1.0.3 → 1.0.4 upgrade
//     was exactly that (20 plugins both sides) plus bug fixes;
//  3. re-check anything reasoning about CORE INTERNALS still holds (the
//     single global signal store, the `_`-excluded request default, whether
//     `data-scope-children` has a listener). None of those are in the docs;
//  4. run the browser smoke — a bundle is the one dependency where "it
//     compiles" proves nothing.
func TestDatastarCoreVersionIsPinned(t *testing.T) {
	t.Parallel()
	raw, err := StaticFS().Open("datastar.js")
	if err != nil {
		t.Fatalf("open embedded datastar.js: %v", err)
	}
	defer func() { _ = raw.Close() }()
	buf := make([]byte, 64)
	n, _ := raw.Read(buf)
	banner := string(buf[:n])
	want := "// Datastar " + pinnedDatastarCore
	if !strings.HasPrefix(banner, want) {
		t.Errorf("vendored Datastar core is not %s:\n  banner: %q\n"+
			"Update pinnedDatastarCore together with the file, and re-verify the "+
			"core-internals notes in skills/gogogo-coding-standards/references/datastar.md",
			pinnedDatastarCore, banner)
	}
}

// TestDatastarCoreHasNoSignalScoping pins the absence that shapes component
// design, so an upgrade that ADDS scoping is a red test with a comment
// telling the upgrader what to revisit — rather than a silent change that
// makes the prefix-every-signal rule look like superstition.
//
// The check is the plugin signature, not a doc claim: Datastar's `signals`
// plugin would need the element to scope by subtree, and it destructures only
// {key, mods, rx}. If a future core adds an `el`/scope binding here, this
// fails and the note is due for a rewrite.
func TestDatastarCoreHasNoSignalScoping(t *testing.T) {
	t.Parallel()
	data, err := readStatic("datastar.js")
	if err != nil {
		t.Fatalf("read datastar.js: %v", err)
	}
	// The signals plugin's apply signature, as minified in v1.0.4.
	const noElementBinding = `name:"signals",returnsValue:!0,apply({key:`
	if !strings.Contains(data, noElementBinding) {
		t.Errorf("the signals plugin no longer matches %q — the core may now bind "+
			"the element and scope signals per subtree. Re-read the scoping notes "+
			"in skills/gogogo-coding-standards/references/datastar.md before "+
			"changing per-component signal naming", noElementBinding)
	}
	// `data-scope-children` is dispatched for third-party plugins. A listener
	// appearing in core would mean scoping became a real, documented feature.
	if strings.Count(data, "datastar-scope-children") != 1 {
		t.Errorf("datastar-scope-children now appears %d times (was 1: the constant "+
			"only) — the core may have grown a real scoping implementation; revisit "+
			"the notes in skills/gogogo-coding-standards/references/datastar.md",
			strings.Count(data, "datastar-scope-children"))
	}
}

// readStatic reads one embedded static file as a string.
func readStatic(name string) (string, error) {
	b, err := fs.ReadFile(StaticFS(), name)
	if err != nil {
		return "", err
	}
	return string(b), nil
}

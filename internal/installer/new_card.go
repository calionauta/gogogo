// Installer engine: the domain card `gogogo new` writes.
//
// The card is the slice's written boundary (docs/domain/<name>.md): what it
// owns, what it deliberately does not, the words it uses and the rules its code
// must keep. It lives in its own file because the scaffold sources are already
// at the file-size gate.
package installer

import "strings"

// newCardSource renders docs/domain/<name>.md: the slice's written boundary.
//
// It starts true rather than empty. `make check-domains` refuses a card whose
// parts are headings, so the scaffolded card names the two routes this run
// actually registers and the rules the generated handler already keeps; the
// "does not own" line is the one the author rewrites first, because a boundary
// nobody named is the one a second model of the domain grows through.
func newCardSource(name string) string {
	return strings.ReplaceAll(newCardTemplate, "{{name}}", name)
}

// newCardTemplate is the card body. The last invariant is deliberately about
// the card itself: the scaffold cannot keep it current, the author can.
var newCardTemplate = strings.Join([]string{
	"# {{name}}",
	"",
	"**Owns.** The {{name}} slice: the auth-gated page at `GET /{{name}}` and the JSON probe at " +
		"`GET /api/{{name}}/ping`. Replace the placeholder noun with the word this slice decides " +
		"about — the card, the handler names and the prompt all use it.",
	"",
	"**Does not own.** Nothing yet. Name the nearest wrong neighbour before you add the first " +
		"field: the slice that looks like it belongs here is where a second model of this noun grows.",
	"",
	"## Language",
	"",
	"| term | means | not |",
	"|---|---|---|",
	"| {{name}} | TODO: the noun, owner-scoped | TODO: the nearest thing it is not |",
	"",
	"## Invariants",
	"",
	"- The owner is stamped from the session; no request field can set it.",
	"- A delete goes through this slice's `canDelete*` check with the caller's role, never a " +
		"role test in the handler.",
	"- A new route, field or stored column updates this card in the same commit.",
	"",
}, "\n")

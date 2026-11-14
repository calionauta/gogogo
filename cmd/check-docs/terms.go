package main

// The ubiquitous language is enforced where it can be.
//
// A glossary that nobody checks is decoration: the vocabulary drifts back the
// moment nobody is looking. So the glossary carries two things, and only one
// of them is prose:
//
//   - the MEANING of each term (docs/glossary.md), which no machine can judge
//     — that is the human layer, like the rest of the docs;
//   - the RETIRED words, which are mechanical: a compound form that was
//     renamed must not come back through a page, a skill or the agent index.
//
// The retired list is deliberately made of COMPOUND forms, never bare words.
// "extension" is still the right word for a file extension and for a public
// extension point; it is only wrong as the old name of a recipe.

import (
	"fmt"
	"slices"
	"strings"
)

// retiredTerm is one name that must not come back, with the word that
// replaced it.
type retiredTerm struct {
	// old is the compound form to reject.
	old string
	// now is what to say instead.
	now string
}

// retiredTerms is the closed list of names the glossary retires. It is
// closed on purpose: a new retired word is added HERE, in review, with the
// commit that retired it — not discovered later by a reader.
var retiredTerms = []retiredTerm{
	{old: "gogogo extension", now: "gogogo recipe"},
	{old: "_extensions/", now: "catalog/ (reviewed entry) or .recipes/ (receipt)"},
	{old: "extensions.gen.go", now: "recipes.gen.go"},
	{old: "extension_list", now: "recipe_list"},
	{old: "extension_describe", now: "recipe_describe"},
	{old: "extension_invoke", now: "recipe_invoke"},
	// The removal class was `plugin` in code and `servant` in prose: two names
	// for one level, which is how a reader ends up translating instead of
	// reading. The class is `servant` everywhere now. The bare word stays
	// legal (a DaisyUI plugin, a provider plugin) — only the class forms go.
	{old: "kind: plugin", now: "kind: servant"},
	{old: "kind `plugin`", now: "kind `servant`"},
	{old: "plugin-kind", now: "servant-kind"},
	{old: "`plugin` row", now: "`servant` row"},
	{old: "`plugin` unit", now: "`servant` unit"},
	{old: "Kind: plugin", now: "Kind: servant"},
}

// checkRetiredTerms rejects a retired name in the prose a reader or an agent
// reads. The code is not scanned: a negative test names the old verb on
// purpose, and the registry is where a name is settled.
func checkRetiredTerms(root string, files []string) []string {
	var out []string
	for _, rel := range files {
		if retiredTermExempt(rel) {
			continue
		}
		lines, err := fileLines(root, rel)
		if err != nil {
			continue
		}
		for i, line := range lines {
			for _, t := range retiredTerms {
				if !strings.Contains(line, t.old) {
					continue
				}
				out = append(out, fmt.Sprintf(
					"%s:%d: %q is a retired name — say %q (docs/glossary.md)",
					rel, i+1, t.old, t.now))
			}
		}
	}
	return out
}

// retiredTermExempt reports whether a file may name a retired term on
// purpose: a dated evaluation records the past, a decision record names what
// it rejected, and the glossary DEFINES the retired names — that is its job.
func retiredTermExempt(rel string) bool {
	return strings.HasPrefix(rel, "docs/research/") ||
		strings.HasPrefix(rel, "docs/migrations/") ||
		rel == glossaryPage ||
		slices.Contains(historyDocs, rel)
}

// glossaryPage is the definition page: it names every retired term on
// purpose, so the rules about invented commands and retired words do not
// apply to it.
const glossaryPage = "docs/glossary.md"

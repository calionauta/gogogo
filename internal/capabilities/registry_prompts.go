// The prompt library's registry entry.
//
// Split from registry.go for the file-sizes gate; All appends it below.
package capabilities

var promptUnits = []Capability{
	{
		ID:      "prompts",
		Kind:    KindCore,
		Summary: "prompt library: .prompt files (Dotprompt shape) and their loader",
		Dirs:    []string{"internal/prompts"},
		Reason: "Every model-facing surface (genui, review, llm suggest, the " +
			"installer's AGENTS.md) loads its instructions from these files. " +
			"Removing the loader leaves the prompts with nowhere to live; " +
			"the check-prompts gate keeps the tree honest.",
	},
}

package gorules

// Slog fixtures: bare slog.Error/Warn in a chi slice must fire; the same
// call outside chi slices, and clean files, must stay quiet. Split from
// rules_test.go at the 500-line file budget; same package.

// obsFixtures are the SlogBypassesObservability cases. subdir drives the
// fixture's package path (PkgPath), which is what the rule scopes on —
// the bad case only fires under features/<x>/chi.
func obsFixtures() []ruleFixture {
	return []ruleFixture{
		{
			name: "bare slog.Error in a chi slice is flagged",
			source: "package fixture\n\nimport \"log/slog\"\n\n" +
				"func bad() {\n\tslog.Error(\"boom\", \"error\", nil)\n}\n",
			wantAtLeast: 1,
			subdir:      "features/todo/chi",
		},
		{
			name: "same slog call outside chi slices stays quiet",
			source: "package fixture\n\nimport \"log/slog\"\n\n" +
				"func elsewhere() {\n\tslog.Error(\"boom\", \"error\", nil)\n}\n",
			wantAtLeast: 0,
		},
		{
			name:        "clean chi slice file stays quiet",
			source:      "package fixture\n\nfunc clean() {}\n",
			wantAtLeast: 0,
			subdir:      "features/todo/chi",
		},
	}
}

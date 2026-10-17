package gorules

// Stdout + HTML fixtures: logging without the structured logger, and markup
// assembled by fmt.Sprintf, must fire; the io.Writer form, format strings that
// only look tag-shaped, and test files must stay quiet. Split from
// rules_obs_test.go for the 500-line file budget; same package.
//
// Every fixture function name here is unique across the whole fixture module
// (all fixtures land in ONE package, so a repeated `bad` is a compile error
// that suppresses every rule — the harness fails loudly on it). The names are
// deliberately descriptive rather than `bad`/`fine`.

// stdoutFixtures are the StdoutBypassesSlog cases.
func stdoutFixtures() []ruleFixture {
	return []ruleFixture{
		{
			name: "fmt.Println is flagged",
			source: "package fixture\n\nimport \"fmt\"\n\n" +
				"func stdoutPrintln() {\n\tfmt.Println(\"boom\")\n}\n",
			wantAtLeast: 1,
		},
		{
			name: "fmt.Printf is flagged",
			source: "package fixture\n\nimport \"fmt\"\n\n" +
				"func stdoutPrintf() {\n\tfmt.Printf(\"%s\\n\", \"boom\")\n}\n",
			wantAtLeast: 1,
		},
		{
			name: "log.Fatalf is flagged",
			source: "package fixture\n\nimport \"log\"\n\n" +
				"func stdlogFatalf() {\n\tlog.Fatalf(\"boom %d\", 1)\n}\n",
			wantAtLeast: 1,
		},
		{
			// The carve-out that keeps the rule honest: writing to an
			// io.Writer the CALLER chose is rendering, not logging, and
			// internal/components relies on it.
			name: "writing to a chosen io.Writer stays quiet",
			source: "package fixture\n\nimport (\n\t\"fmt\"\n\t\"io\"\n)\n\n" +
				"func writesToChosenWriter(w io.Writer, s string) {\n\tfmt.Fprintf(w, \"%s\", s)\n}\n",
			wantAtLeast: 0,
		},
		{
			name: "fmt.Sprintf is not printing",
			source: "package fixture\n\nimport \"fmt\"\n\n" +
				"func sprintfIsNotLogging(n int) string {\n\treturn fmt.Sprintf(\"n=%d\", n)\n}\n",
			wantAtLeast: 0,
		},
	}
}

// htmlFixtures are the HTMLInSprintf cases.
func htmlFixtures() []ruleFixture {
	return []ruleFixture{
		{
			name: "a div with a class is flagged",
			source: "package fixture\n\nimport \"fmt\"\n\n" +
				"func htmlDivWithClass(t string) string {\n\treturn fmt.Sprintf(`<div class=\"%s\">`, t)\n}\n",
			wantAtLeast: 1,
		},
		{
			name: "a closing tag is flagged",
			source: "package fixture\n\nimport \"fmt\"\n\n" +
				"func htmlClosingTag(t string) string {\n\treturn fmt.Sprintf(`<span>%s</span>`, t)\n}\n",
			wantAtLeast: 1,
		},
		{
			name: "a plain format string stays quiet",
			source: "package fixture\n\nimport \"fmt\"\n\n" +
				"func sprintfPlainProse(s string) string {\n\treturn fmt.Sprintf(\"value %s holds no markup\", s)\n}\n",
			wantAtLeast: 0,
		},
		{
			// A format string with no attribute and no closing form must not
			// match: a column layout is not HTML.
			name: "an aligned column format stays quiet",
			source: "package fixture\n\nimport \"fmt\"\n\n" +
				"func sprintfAlignedColumn(name string, w int) string {\n\treturn fmt.Sprintf(\"%-*s\", w, name)\n}\n",
			wantAtLeast: 0,
		},
	}
}

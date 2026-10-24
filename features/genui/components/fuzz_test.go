// SCOPE:feature — Fuzz seed for the model-HTML sanitizer.
package components

import (
	"strings"
	"testing"
)

func FuzzSanitizeHTML(f *testing.F) {
	for _, seed := range []string{
		"<div>ok</div>",
		`<script>alert(1)</script>`,
		`<a href="javascript:alert(1)">x</a>`,
		`<img src=x onerror=alert(1)>`,
		"",
	} {
		f.Add(seed)
	}
	f.Fuzz(func(_ *testing.T, raw string) {
		// The contract is "never panic, never return active content on
		// success"; the allowlist tests pin the policy itself.
		clean, err := SanitizeHTML(raw)
		if err != nil {
			return
		}
		for _, bad := range []string{"<script", "<iframe", "javascript:", "onerror=", "<form"} {
			if strings.Contains(strings.ToLower(clean), bad) {
				panic("sanitizer returned active content: " + bad)
			}
		}
	})
}

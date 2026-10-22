// SCOPE:plugin — the model-markup class vocabulary gate.
package components

import (
	"os"
	"regexp"
	"strings"
	"testing"
)

// cssSafelistRE matches the single @source inline("...") literal a
// Tailwind input stylesheet carries for the model-markup vocabulary.
var cssSafelistRE = regexp.MustCompile(`@source\s+inline\("([^"]*)"\)`)

// cssSafelist reads one stylesheet's safelist as a set. Exactly one
// literal is expected: several would make "which list is the vocabulary"
// ambiguous, which is the drift this gate exists to prevent.
func cssSafelist(t *testing.T, path string) map[string]bool {
	t.Helper()
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v (run from features/genui/components)", path, err)
	}
	m := cssSafelistRE.FindAllStringSubmatch(string(b), -1)
	if len(m) != 1 {
		t.Fatalf("%s: found %d @source inline(...) literals, want exactly 1", path, len(m))
	}
	out := map[string]bool{}
	for c := range strings.FieldsSeq(m[0][1]) {
		out[c] = true
	}
	return out
}

// cssSelector renders the selector form Tailwind emits for a class:
// variant colons and slash modifiers are escaped in the stylesheet.
func cssSelector(class string) string {
	return "." + strings.NewReplacer(":", `\:`, "/", `\/`).Replace(class)
}

// TestModelHTMLClassVocabulary is the red-proof for the unstyled-markup
// report. A model invents html_block markup at runtime, so Tailwind never
// scans its classes; before the @source inline safelist, `opacity-60` and
// `sm:grid-cols-3` — advertised by the prompt and used by the keyless
// demo answer — were absent from app.min.css and the invented visual
// rendered unstyled. Three things must agree: the Go vocabulary the
// prompt shows, each stylesheet's safelist, and each built bundle.
func TestModelHTMLClassVocabulary(t *testing.T) {
	t.Parallel()
	daisyUI := append(append([]string{}, ModelHTMLClasses...), ModelHTMLComponents...)
	cases := []struct {
		name   string
		input  string
		bundle string
		want   []string
	}{
		{"daisyui", "../../../src/css/input.css", "../../../web/resources/static/app.min.css", daisyUI},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			safelist := cssSafelist(t, tc.input)
			want := map[string]bool{}
			for _, c := range tc.want {
				want[c] = true
			}
			for _, c := range tc.want {
				if !safelist[c] {
					t.Errorf("%q is advertised by the html_block prompt but missing from the "+
						"@source inline safelist in %s — add it there", c, tc.input)
				}
			}
			for c := range safelist {
				if !want[c] {
					t.Errorf("%q is safelisted in %s but absent from the prompt vocabulary in "+
						"features/genui/components/htmlclasses.go — a model cannot use a class "+
						"it was never told about", c, tc.input)
				}
			}
			b, err := os.ReadFile(tc.bundle)
			if err != nil {
				t.Fatalf("read %s: %v", tc.bundle, err)
			}
			bundle := string(b)
			for _, c := range tc.want {
				if !strings.Contains(bundle, cssSelector(c)) {
					t.Errorf("%q missing from %s — the safelist changed and the bundle is stale "+
						"(or the class does not exist under this skin): run `make css`",
						c, tc.bundle)
				}
			}
		})
	}
}

// TestChatPromptCarriesTheClassVocabulary pins the other end of the same
// contract: the class list reaches the model. A prompt that names no
// classes and a bundle that styles none fail the same way — markup that
// looks authored and renders plain.
func TestChatPromptCarriesTheClassVocabulary(t *testing.T) {
	t.Parallel()
	prompt := PromptForChat()
	for _, want := range []string{"classes ONLY", "grid-cols-3", "card", "badge", "alert", "opacity-60"} {
		if !strings.Contains(prompt, want) {
			t.Errorf("chat prompt missing %q — the model is answering outside the styled vocabulary", want)
		}
	}
	if strings.Contains(prompt, "timelines") {
		t.Errorf("chat prompt still advertises the retired loose class prose")
	}
}

// SCOPE:plugin — html_block sanitizer tests (red-proof).
package components

import (
	"strings"
	"testing"
)

// TestSanitizeHTMLDropsActiveContent pins the security contract: model
// markup keeps structure + classes + safe links; scripts, handlers,
// styles, media, comments and dangerous URLs never survive. Red-proof:
// allow one of them through and the corresponding case goes red.
func TestSanitizeHTMLDropsActiveContent(t *testing.T) {
	t.Parallel()
	big := "<div>" + strings.Repeat("x", maxHTMLBytes) + "</div>"
	for _, tc := range []struct {
		name    string
		in      string
		want    []string
		notWant []string
		wantErr bool
	}{
		{
			name:    "script dropped with subtree",
			in:      `<div class="p-4"><script>alert(1)</script><p>hi</p></div>`,
			want:    []string{`<div class="p-4">`, "<p>hi</p>"},
			notWant: []string{"script", "alert"},
		},
		{
			name:    "event handlers and style dropped, classes kept",
			in:      `<div onclick="evil()" style="color:red" class="grid grid-cols-2"><span>ok</span></div>`,
			want:    []string{`class="grid grid-cols-2"`, "<span>ok</span>"},
			notWant: []string{"onclick", "style="},
		},
		{
			name:    "javascript href neutered, safe links kept",
			in:      `<a href="javascript:alert(1)">x</a><a href="https://example.com">y</a><a href="/todo">z</a>`,
			want:    []string{`href="https://example.com"`, `href="/todo"`},
			notWant: []string{"javascript:"},
		},
		{
			name:    "media and forms dropped, text survives escaped",
			in:      `<img src="x"><form><input></form><p>a < b</p><!-- secret -->`,
			want:    []string{"a &lt; b"},
			notWant: []string{"<img", "<form", "<input", "<!--", "secret"},
		},
		{
			name:    "unknown tags dropped, tables kept",
			in:      `<table><tr><td>v</td></tr></table><blink>no</blink>`,
			want:    []string{"<table>", "<td>v</td>", "no"},
			notWant: []string{"<blink"},
		},
		{
			name:    "empty rejected",
			in:      `   `,
			wantErr: true,
		},
		{
			name:    "oversized rejected",
			in:      big,
			wantErr: true,
		},
		{
			name:    "sanitized-to-nothing rejected",
			in:      `<script>alert(1)</script>`,
			wantErr: true,
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			got, err := SanitizeHTML(tc.in)
			if tc.wantErr {
				if err == nil {
					t.Fatalf("SanitizeHTML(%q) = %q, want error", tc.in, got)
				}
				return
			}
			if err != nil {
				t.Fatalf("SanitizeHTML: %v", err)
			}
			for _, w := range tc.want {
				if !strings.Contains(got, w) {
					t.Errorf("missing %q in %q", w, got)
				}
			}
			for _, nw := range tc.notWant {
				if strings.Contains(got, nw) {
					t.Errorf("forbidden %q survives in %q", nw, got)
				}
			}
		})
	}
}

// TestSanitizeHTMLClosesClassVocabulary pins the CopilotKit sandbox lesson:
// the open-class pass-through let the model style with classes that render
// unstyled (Tailwind never emitted them). Unknown classes are dropped, the
// advertised vocabulary survives. Red-proof: allow everything through and
// the unknown class survives.
func TestSanitizeHTMLClosesClassVocabulary(t *testing.T) {
	t.Parallel()
	got, err := SanitizeHTML(`<div class="p-4 grid invented-thing text-neon">hi</div>`)
	if err != nil {
		t.Fatalf("SanitizeHTML: %v", err)
	}
	for _, want := range []string{"p-4", "grid"} {
		if !strings.Contains(got, want) {
			t.Errorf("allowed class %q dropped in %q", want, got)
		}
	}
	for _, notWant := range []string{"invented-thing", "text-neon"} {
		if strings.Contains(got, notWant) {
			t.Errorf("unknown class %q survives in %q", notWant, got)
		}
	}
}

// TestCatalogHTMLBlockPinsChatOnly pins the split: html_block resolves
// in the shared registry (both features render it), but only the chat
// prompt advertises it — the Ask showcase stays catalog-only. The split
// is a direction, not a list: chat must LEAD with the invented visual
// (html_block advertised before any catalog component), or the model
// answers in the catalog again and the two features read as one — the
// complaint that produced this test.
func TestCatalogHTMLBlockPinsChatOnly(t *testing.T) {
	t.Parallel()
	if !Registered("html_block") {
		t.Fatal("html_block must resolve in the registry")
	}
	if strings.Contains(PromptForSurfaces([]Surface{SurfaceCatalog}), "html_block") {
		t.Error("Ask prompt must not advertise html_block (catalog-only showcase)")
	}
	chat := PromptForChat()
	for _, want := range []string{"html_block", "visual"} {
		if !strings.Contains(chat, want) {
			t.Errorf("chat prompt missing %q", want)
		}
	}
	invented, catalog := strings.Index(chat, "html_block"), strings.Index(chat, "text_note")
	if invented < 0 || catalog < 0 || invented > catalog {
		t.Errorf("chat prompt must advertise html_block before the catalog components (%d vs %d)", invented, catalog)
	}
	raw := `{"components":[{"type":"html_block",` +
		`"props":{"html":"<div class=\"p-2\"><script>x</script><p>hi</p></div>"}}]}`
	dirs, err := ParseDirectives(raw)
	if err != nil {
		t.Fatalf("ParseDirectives html_block: %v", err)
	}
	if len(dirs) != 1 || dirs[0].Component != "html_block" {
		t.Fatalf("parsed = %+v, want one html_block", dirs)
	}
}

// SCOPE:plugin — the toggle matrix: what survives per surface set.
//
// The composer toggles ride every request, so the filter is the last word on
// "the tab turned this band off": an answer reaching for a disabled band is
// dropped (the repair loop had its chance), and an answer left with nothing
// becomes the honest line in the worker — never a silent hole, never a
// component the prompt never named.
package components

import (
	"testing"
)

func filterNames(dirs []Directive) []string {
	out := make([]string, 0, len(dirs))
	for _, d := range dirs {
		out = append(out, d.Component)
	}
	return out
}

func dirFor(component string) Directive {
	return Directive{Component: component, Props: map[string]any{}}
}

// TestFilterDirectivesKeepsOnlyTheEnabledVocabulary is the toggle contract
// in one table: every band off drops its components, and prose-only (empty)
// keeps nothing — the worker replaces the hole with the honest line.
func TestFilterDirectivesKeepsOnlyTheEnabledVocabulary(t *testing.T) {
	t.Parallel()
	html := dirFor(htmlBlockName)
	note := Directive{Component: textNoteName, Props: map[string]any{"text": "hi"}}
	panel := Directive{Component: panelNameInTest, Props: map[string]any{"filter": "open"}}
	table := Directive{Component: dataTableName, Props: map[string]any{}}
	invented := dirFor("invented_thing")

	cases := map[string]struct {
		surfaces []Surface
		dirs     []Directive
		want     []string
	}{
		"all on keeps everything known": {
			surfaces: nil, dirs: []Directive{html, note, panel, table},
			want: []string{htmlBlockName, textNoteName, panelNameInTest, dataTableName},
		},
		"catalog-only drops invented markup and panels": {
			surfaces: []Surface{SurfaceCatalog, SurfaceNative},
			dirs:     []Directive{html, note, panel, table},
			want:     []string{textNoteName, panelNameInTest, dataTableName},
		},
		"native-only keeps just the panel": {
			surfaces: []Surface{SurfaceNative},
			dirs:     []Directive{html, note, panel, table},
			want:     []string{panelNameInTest},
		},
		"custom-only keeps just invented markup": {
			surfaces: []Surface{SurfaceCustom},
			dirs:     []Directive{html, note, panel, table},
			want:     []string{htmlBlockName},
		},
		"all off keeps nothing": {
			surfaces: []Surface{},
			dirs:     []Directive{html, note, panel, table},
			want:     []string{},
		},
		"unknown names never survive": {
			surfaces: nil, dirs: []Directive{invented},
			want: []string{},
		},
	}
	for name, tc := range cases {
		got := filterNames(FilterDirectives(tc.dirs, tc.surfaces))
		if len(got) != len(tc.want) {
			t.Errorf("%s: kept %v, want %v", name, got, tc.want)
			continue
		}
		for i := range got {
			if got[i] != tc.want[i] {
				t.Errorf("%s: kept %v, want %v", name, got, tc.want)
				break
			}
		}
	}
}

// TestFilterDirectivesPrunesNestedChildren pins the recursion: a section
// holding a disabled child keeps the section with the surviving child, and
// a section left with nothing is dropped — a titled box around nothing is
// the half-render the catalog refuses.
func TestFilterDirectivesPrunesNestedChildren(t *testing.T) {
	t.Parallel()
	section := Directive{
		Component: sectionName,
		Props:     map[string]any{"title": "Week"},
		Children: []Directive{
			{Component: textNoteName, Props: map[string]any{"text": "lead"}},
			dirFor(htmlBlockName),
		},
	}
	catalogOnly := []Surface{SurfaceCatalog, SurfaceNative}
	kept := FilterDirectives([]Directive{section}, catalogOnly)
	if len(kept) != 1 || len(kept[0].Children) != 1 || kept[0].Children[0].Component != textNoteName {
		t.Fatalf("nested filter kept %+v, want the section with only the text_note child", kept)
	}
	if kept := FilterDirectives([]Directive{section}, []Surface{}); len(kept) != 0 {
		t.Fatalf("all-off filter kept %+v, want nothing", kept)
	}
}

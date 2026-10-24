// SCOPE:plugin — the surface declaration's contract: parsing, the native
// invariant, and the per-leg vocabularies.
package components

import (
	"slices"
	"strings"
	"testing"
)

// TestParseSurfaces pins the operator-facing CSV: named bands parse, order is
// normalized to advertisement order, duplicates collapse, and — the important
// half — an unknown name is an ERROR. A typo here changes what the model may
// emit, so a silent drop would be a surface change with no signal at all.
// Every band is optional, including native: "none" is the explicit prose-only
// set, and naming one band never smuggles another in.
func TestParseSurfaces(t *testing.T) {
	t.Parallel()
	cases := map[string]struct {
		csv     string
		def     []Surface
		want    []Surface
		wantErr bool
	}{
		"empty keeps the default": {
			csv: "", def: DefaultSurfacesChat,
			want: []Surface{SurfaceCustom, SurfaceCatalog, SurfaceNative},
		},
		"whitespace keeps the default": {
			csv: "   ", def: DefaultSurfacesChat,
			want: []Surface{SurfaceCustom, SurfaceCatalog, SurfaceNative},
		},
		"order is normalized": {csv: "native,catalog", def: nil, want: []Surface{SurfaceCatalog, SurfaceNative}},
		"duplicates collapse": {csv: "catalog,catalog", def: nil, want: []Surface{SurfaceCatalog}},
		"one band stays one band": {
			csv: "catalog", def: nil, want: []Surface{SurfaceCatalog},
		},
		"case is ignored":          {csv: "CUSTOM", def: nil, want: []Surface{SurfaceCustom}},
		"unknown name is an error": {csv: "catalog,bogus", def: nil, wantErr: true},
		"only native stays native": {
			csv: "native", def: nil, want: []Surface{SurfaceNative},
		},
		"none is prose-only": {
			csv: "none", def: nil, want: []Surface{},
		},
		"prose is prose-only": {
			csv: "prose", def: nil, want: []Surface{},
		},
	}
	for name, tc := range cases {
		got, err := ParseSurfaces(tc.csv, tc.def)
		if tc.wantErr {
			if err == nil {
				t.Errorf("%s: parsed %v, want an error", name, got)
			}
			continue
		}
		if err != nil {
			t.Errorf("%s: %v", name, err)
			continue
		}
		if !slices.Equal(got, tc.want) {
			t.Errorf("%s: got %v, want %v", name, Names(got), Names(tc.want))
		}
	}
}

// TestNilMeansDefault pins what a Go caller gets for passing nothing: the
// Chat default (every band), not a subset. An explicit empty slice is the
// opposite — prose-only, respected as written.
func TestNilMeansDefault(t *testing.T) {
	t.Parallel()
	if got := NormalizeSurfaces(nil); !slices.Equal(got, []Surface{SurfaceCustom, SurfaceCatalog, SurfaceNative}) {
		t.Errorf("NormalizeSurfaces(nil) = %v, want the Chat default", Names(got))
	}
	if got := NormalizeSurfaces([]Surface{}); len(got) != 0 {
		t.Errorf("NormalizeSurfaces(empty) = %v, want prose-only", Names(got))
	}
	if got := PromptForSurfaces([]Surface{}); !strings.Contains(got, "plain prose") {
		t.Errorf("prose-only prompt is not prose-only:\n%s", got)
	}
	if got := CatalogComponentsFor([]Surface{}); len(got) != 0 {
		t.Errorf("prose-only catalog offers %v, want nothing", got)
	}
}

// TestValidateSurfacesRejectsUnnamedBands closes the hole a string-backed enum
// leaves open: NormalizeSurfaces (a normalizer) DROPS an unknown value, so a
// Go caller constructing a set from literals could hand a leg a vocabulary it
// cannot name — and the only visible symptom would be a band quietly missing
// from the prompt. ValidateSurfaces is the assertion; this pins that it
// actually rejects, and that the valid set passes.
func TestValidateSurfacesRejectsUnnamedBands(t *testing.T) {
	t.Parallel()
	if err := ValidateSurfaces([]Surface{SurfaceCatalog, SurfaceNative, SurfaceCustom}); err != nil {
		t.Errorf("the declared bands were rejected: %v", err)
	}
	if err := ValidateSurfaces([]Surface{SurfaceCatalog, Surface("custon")}); err == nil {
		t.Error("a typo passed ValidateSurfaces, want it rejected")
	}
	if err := ValidateSurfaces([]Surface{Surface("")}); err == nil {
		t.Error("the empty string passed ValidateSurfaces, want it rejected")
	}
}

// TestNormalizeDropsUnknown documents the asymmetry ValidateSurfaces exists
// to catch: the normalizer cannot return an error, so it drops. If this ever
// starts rejecting instead, both call sites need revisiting.
func TestNormalizeDropsUnknown(t *testing.T) {
	t.Parallel()
	got := NormalizeSurfaces([]Surface{Surface("bogus"), SurfaceCatalog})
	if slices.Contains(got, Surface("bogus")) {
		t.Errorf("NormalizeSurfaces kept an unknown band: %v", Names(got))
	}
	if !slices.Equal(got, []Surface{SurfaceCatalog}) {
		t.Errorf("NormalizeSurfaces changed a real band: %v", Names(got))
	}
	if strings.Contains(PromptForSurfaces([]Surface{Surface("bogus"), SurfaceCatalog}), "bogus") {
		t.Error("an unknown band reached the prompt")
	}
}

// TestCustomCanBeTurnedOff is the toggle the operator question is about:
// Custom off removes the invented-markup surface from BOTH the prompt and the
// router's candidate set, while the bands left on survive. Red-proof: drop
// the HasSurfaces(SurfaceCustom) check in catalogSurfaceFragment and the
// html_block assertion fails.
func TestCustomCanBeTurnedOff(t *testing.T) {
	t.Parallel()
	off, err := ParseSurfaces("catalog,native", nil)
	if err != nil {
		t.Fatal(err)
	}
	prompt := PromptForSurfaces(off)
	if strings.Contains(prompt, htmlBlockName) {
		t.Errorf("custom off still advertises %s:\n%s", htmlBlockName, prompt)
	}
	if slices.Contains(CatalogComponentsFor(off), htmlBlockName) {
		t.Error("custom off still offers html_block to the router")
	}
	// The bands left on survive.
	for _, want := range []string{"data_table", sectionName, panelNameInTest} {
		if !strings.Contains(prompt, want) {
			t.Errorf("custom off dropped %q, want only the invented-markup band off:\n%s", want, prompt)
		}
	}
}

// TestCatalogCanBeTurnedOff is the other toggle: with only custom enabled the
// model still has a vocabulary (html_block) and nothing else — no leaves,
// no panels.
func TestCatalogCanBeTurnedOff(t *testing.T) {
	t.Parallel()
	off, err := ParseSurfaces("custom", nil)
	if err != nil {
		t.Fatal(err)
	}
	prompt := PromptForSurfaces(off)
	if !strings.Contains(prompt, htmlBlockName) {
		t.Error("custom on does not advertise html_block")
	}
	if strings.Contains(prompt, "data_table") {
		t.Errorf("catalog off still advertises data_table:\n%s", prompt)
	}
	if strings.Contains(prompt, panelNameInTest) {
		t.Errorf("native off still advertises %q:\n%s", panelNameInTest, prompt)
	}
}

// TestNativeCanBeTurnedOff closes the loop: native off removes the panels
// from the prompt and the router set while the generative bands survive —
// and all three off is prose-only, still a working chat.
func TestNativeCanBeTurnedOff(t *testing.T) {
	t.Parallel()
	off, err := ParseSurfaces("catalog,custom", nil)
	if err != nil {
		t.Fatal(err)
	}
	prompt := PromptForSurfaces(off)
	if strings.Contains(prompt, panelNameInTest) {
		t.Errorf("native off still advertises %q:\n%s", panelNameInTest, prompt)
	}
	for _, want := range []string{htmlBlockName, "data_table", sectionName} {
		if !strings.Contains(prompt, want) {
			t.Errorf("native off dropped %q, want only the panels off:\n%s", want, prompt)
		}
	}
	prose, err := ParseSurfaces("none", nil)
	if err != nil {
		t.Fatal(err)
	}
	if got := PromptForSurfaces(prose); !strings.Contains(got, "plain prose") {
		t.Errorf("all-off prompt is not prose-only:\n%s", got)
	}
	if got := CatalogComponentsFor(prose); len(got) != 0 {
		t.Errorf("all-off catalog offers %v, want nothing", got)
	}
}

// TestLegPromptsDifferByCustomOnly pins what actually distinguishes the two
// legs at the prompt: the custom band, and nothing else. If this ever fails
// because the demo grew the escape hatch, the surfaces have
// collapsed into one and that is a product decision, not a refactor.
func TestLegPromptsDifferByCustomOnly(t *testing.T) {
	t.Parallel()
	strip := func(explicit bool) string {
		set, err := ParseSurfaces(map[bool]string{true: "catalog,native", false: "custom,catalog,native"}[explicit], nil)
		if err != nil {
			t.Fatal(err)
		}
		return PromptForSurfaces(set)
	}
	withCustom := strip(true)
	withoutCustom := strip(false)
	if withCustom == withoutCustom {
		t.Fatal("the custom band changed nothing in the prompt")
	}
	// Every non-custom band is present in both.
	bands := []string{
		textNoteName, planCardsName, dataTableName, barChartName, actionButtonsName,
		sectionName, panelNameInTest,
	}
	for _, fragment := range bands {
		if !strings.Contains(withoutCustom, fragment) {
			t.Errorf("catalog-only prompt lost %q:\n%s", fragment, withoutCustom)
		}
		if !strings.Contains(withCustom, fragment) {
			t.Errorf("custom prompt lost %q:\n%s", fragment, withCustom)
		}
	}
}

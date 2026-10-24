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
// half — an unknown name or an empty result is an ERROR. A typo here changes
// what the model may emit, so a silent drop would be a surface change with no
// signal at all.
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
		"order is normalized":            {csv: "native,catalog", def: nil, want: []Surface{SurfaceCatalog, SurfaceNative}},
		"duplicates collapse":            {csv: "catalog,catalog", def: nil, want: []Surface{SurfaceCatalog, SurfaceNative}},
		"native is appended when absent": {csv: "catalog", def: nil, want: []Surface{SurfaceCatalog, SurfaceNative}},
		"case is ignored":                {csv: "CUSTOM", def: nil, want: []Surface{SurfaceCustom, SurfaceNative}},
		"unknown name is an error":       {csv: "catalog,bogus", def: nil, wantErr: true},
		"only native falls back to catalog": {
			csv: "native", def: nil, want: []Surface{SurfaceCatalog, SurfaceNative},
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
	if !slices.Contains(got, SurfaceCatalog) || !slices.Contains(got, SurfaceNative) {
		t.Errorf("NormalizeSurfaces dropped a real band: %v", Names(got))
	}
	if strings.Contains(PromptForSurfaces([]Surface{Surface("bogus"), SurfaceCatalog}), "bogus") {
		t.Error("an unknown band reached the prompt")
	}
}

// TestCustomCanBeTurnedOff is the toggle the operator question is about:
// Custom off must remove the invented-markup surface from BOTH the prompt and
// the router's candidate set, while leaving the catalog and native intact.
// Red-proof: drop the HasSurfaces(SurfaceCustom) check in
// catalogSurfaceFragment and the html_block assertion fails.
func TestCustomCanBeTurnedOff(t *testing.T) {
	t.Parallel()
	off, err := ParseSurfaces("catalog", nil)
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
	// The rest of the vocabulary survives.
	for _, want := range []string{"data_table", sectionName, panelNameInTest} {
		if !strings.Contains(prompt, want) {
			t.Errorf("custom off dropped %q, want only the invented-markup band off:\n%s", want, prompt)
		}
	}
}

// TestCatalogCanBeTurnedOff is the other toggle: with only custom enabled the
// model still has a vocabulary (html_block) and the native band, and no leaves.
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
	if !strings.Contains(prompt, panelNameInTest) {
		t.Error("native was lost with the catalog off; native is not a toggle")
	}
}

// TestLegPromptsDifferByCustomOnly pins what actually distinguishes the two
// legs at the prompt: the custom band, and nothing else. If this ever fails
// because the demo grew the escape hatch, the surfaces have
// collapsed into one and that is a product decision, not a refactor.
func TestLegPromptsDifferByCustomOnly(t *testing.T) {
	t.Parallel()
	strip := func(explicit bool) string {
		set, err := ParseSurfaces(map[bool]string{true: "catalog", false: "custom,catalog"}[explicit], nil)
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

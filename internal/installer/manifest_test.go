package installer

// Manifest and plan tests: unit selection, prompt parsing, plan output.

import (
	"fmt"
	"io"
	"os"
	"strings"
	"testing"

	"github.com/calionauta/gogogo/internal/capabilities"
)

func TestValidateName(t *testing.T) {
	for _, ok := range []string{"my-app", "app2", "a.b_c-d"} {
		if err := validateName(ok); err != nil {
			t.Errorf("validateName(%q) = %v, want nil", ok, err)
		}
	}
	for _, bad := range []string{"", "-app", "my app", "gogogo"} {
		if err := validateName(bad); err == nil {
			t.Errorf("validateName(%q) = nil, want error", bad)
		}
	}
}

func TestPlanTrimKeepsAllByDefault(t *testing.T) {
	keep := parseKeep("", "")
	if got := planTrim(keep); len(got) != 0 {
		t.Errorf("planTrim(default) dropped %v, want nothing", dropIDs(got))
	}
}

// keepAllBut returns a keep set with every manifest unit kept except dropped.
// It keeps test lines short and stays correct when new units are added.
func keepAllBut(dropped ...string) keepSet {
	skip := map[string]bool{}
	for _, d := range dropped {
		skip[d] = true
	}
	var keep []string
	for _, u := range manifestUnits {
		if !skip[u.id] {
			keep = append(keep, u.id)
		}
	}
	joined := strings.Join(keep, ",")
	return parseKeep(joined, joined)
}

func TestParseKeepDimensionsIndependent(t *testing.T) {
	// Convention: empty means keep-all IN THAT DIMENSION. Answering only
	// the plugins question must not drop every feature.
	keep := parseKeep("dagnats", "")
	drop := planTrim(keep)
	ids := map[string]bool{}
	for _, u := range drop {
		ids[u.id] = true
	}
	if ids["whiteboard"] || ids["landing"] || ids["config-view"] {
		t.Errorf("empty features answer dropped features: %v", dropIDs(drop))
	}
	if !ids["credits"] || !ids["sounds"] || !ids["skins-extra"] {
		t.Errorf("unlisted plugins must drop: %v", dropIDs(drop))
	}
}

func TestParseSelection(t *testing.T) {
	options := []string{"dagnats", "credits", "sounds"}
	cases := []struct {
		input   string
		wantIDs []string
		wantAll bool
		wantErr bool
	}{
		{"", nil, true, false},
		{"none", []string{}, false, false},
		{"NONE", []string{}, false, false},
		{"1", []string{"dagnats"}, false, false},
		{"1,3", []string{"dagnats", "sounds"}, false, false},
		{"3,1,3", []string{"sounds", "dagnats"}, false, false},
		{"credits", []string{"credits"}, false, false},
		{"1,credits", []string{"dagnats", "credits"}, false, false},
		{" 2 , sounds ", []string{"credits", "sounds"}, false, false},
		{"0", nil, false, true},
		{"4", nil, false, true},
		{"nats", nil, false, true},
		{"1,nats", nil, false, true},
		{"abc", nil, false, true},
	}
	for _, tc := range cases {
		ids, all, err := parseSelection(tc.input, options)
		if tc.wantErr != (err != nil) {
			t.Errorf("parseSelection(%q) err = %v, wantErr %v", tc.input, err, tc.wantErr)
			continue
		}
		if all != tc.wantAll {
			t.Errorf("parseSelection(%q) all = %v, want %v", tc.input, all, tc.wantAll)
		}
		if fmt.Sprint(ids) != fmt.Sprint(tc.wantIDs) {
			t.Errorf("parseSelection(%q) = %v, want %v", tc.input, ids, tc.wantIDs)
		}
	}
}

func TestUnitMenusFollowRegistryKinds(t *testing.T) {
	// Menu numbers are positional: pin the order so humans learn stable
	// numbers and prompt options never drift from registry kinds.
	plugins := unitIDsOfKind(capabilities.KindPlugin)
	wantPlugins := []string{"dagnats", "goakt", "credits", "sounds", "skins-extra"}
	if fmt.Sprint(plugins) != fmt.Sprint(wantPlugins) {
		t.Errorf("plugin menu = %v, want %v", plugins, wantPlugins)
	}
	features := unitIDsOfKind(capabilities.KindFeature)
	wantFeatures := []string{"whiteboard", "landing", "config-view"}
	if fmt.Sprint(features) != fmt.Sprint(wantFeatures) {
		t.Errorf("feature menu = %v, want %v", features, wantFeatures)
	}
}

func TestPromptFormRepairsInvalidAnswer(t *testing.T) {
	// Humans mistype: an invalid plugins answer re-asks instead of
	// aborting or silently mis-trimming.
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	fmt.Fprintln(w, "my-app")
	fmt.Fprintln(w, "myorg")
	fmt.Fprintln(w, "nats") // invalid: not an installer unit
	fmt.Fprintln(w, "1")    // repair: first plugin (dagnats)
	fmt.Fprintln(w, "")     // features: empty = keep all
	_ = w.Close()
	pr, pw, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	done := make(chan string, 1)
	go func() {
		raw, _ := io.ReadAll(pr)
		done <- string(raw)
	}()
	res := promptForm(r, pw)
	_ = pw.Close()
	transcript := <-done
	if res.plugins != "dagnats" {
		t.Errorf("plugins = %q, want dagnats", res.plugins)
	}
	if res.features != "" {
		t.Errorf("features = %q, want empty (keep all)", res.features)
	}
	if !strings.Contains(transcript, "unknown") || !strings.Contains(transcript, "try again") {
		t.Errorf("transcript must show the repair loop:\n%s", transcript)
	}
}

func TestParseKeepNoneDropsDimension(t *testing.T) {
	keep := parseKeep("none", "none")
	drop := planTrim(keep)
	if len(drop) != len(manifestUnits) {
		t.Errorf("none/none should drop all %d units, dropped %v", len(manifestUnits), dropIDs(drop))
	}
}

func TestPlanTrimDropsSkipped(t *testing.T) {
	keep := parseKeep("landing", "landing")
	drop := planTrim(keep)
	ids := map[string]bool{}
	for _, u := range drop {
		ids[u.id] = true
	}
	if ids["landing"] {
		t.Errorf("landing was kept but appears in drop set")
	}
	if !ids["dagnats"] || !ids["whiteboard"] || !ids["credits"] {
		t.Errorf("expected dagnats/whiteboard/credits in drop set, got %v", dropIDs(drop))
	}
}

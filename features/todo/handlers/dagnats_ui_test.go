package handlers

import (
	"testing"

	"github.com/calionauta/gogogo/config"
)

type stubResumer struct{}

func (stubResumer) ResumeOnboarding(string) {}

// TestDagnatsUIEnabled_RequiresRegistration proves the workflow UI follows
// registration truth, not config truth: the tab, button, and hints render
// only when the engine is enabled AND the onboarding routes were actually
// registered. After the installer trims the dagnats unit, h.onboarding
// stays nil and the UI degrades with no dead button — same as
// DAGNATS_ENABLED=false.
func TestDagnatsUIEnabled_RequiresRegistration(t *testing.T) {
	enabled := &config.Config{}
	enabled.DagNats.Enabled = true
	disabled := &config.Config{}
	disabled.DagNats.Enabled = false

	cases := []struct {
		name string
		h    *TodoHandler
		want bool
	}{
		{"enabled and registered", &TodoHandler{cfg: enabled, onboarding: stubResumer{}}, true},
		{"enabled but never registered (trimmed)", &TodoHandler{cfg: enabled}, false},
		{"disabled but stale resumer", &TodoHandler{cfg: disabled, onboarding: stubResumer{}}, false},
		{"disabled and unregistered", &TodoHandler{cfg: disabled}, false},
		{"nil config never panics", &TodoHandler{}, false},
	}
	for _, tc := range cases {
		if got := tc.h.dagnatsUIEnabled(); got != tc.want {
			t.Errorf("%s: dagnatsUIEnabled = %v, want %v", tc.name, got, tc.want)
		}
	}
}

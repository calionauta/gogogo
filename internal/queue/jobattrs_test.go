package queue

import "testing"

func TestRecipeOfReadsTheSeamPrefix(t *testing.T) {
	cases := []struct{ jobType, want string }{
		{"ext:kiosk:confirm", "kiosk"},
		{"ext:todo:suggest", "todo"},
		{"ext:notes:", "notes"},
		{"todo:suggest", ""},
		{"ext:", ""},
		{"ext::x", ""},
		{"", ""},
	}
	for _, tc := range cases {
		if got := recipeOf(tc.jobType); got != tc.want {
			t.Errorf("recipeOf(%q) = %q, want %q", tc.jobType, got, tc.want)
		}
	}
}

func TestJobAttrsAddsRecipeOnlyForRecipeJobs(t *testing.T) {
	got := jobAttrs("ext:kiosk:confirm")
	if len(got) != 4 || got[2] != "recipe" || got[3] != "kiosk" {
		t.Errorf("jobAttrs(recipe job) = %v, want type + recipe", got)
	}
	if got := jobAttrs("todo:suggest"); len(got) != 2 {
		t.Errorf("jobAttrs(core job) = %v, want type only", got)
	}
}

package ui

import (
	"context"
	"testing"
)

func TestNavEventNameBounded(t *testing.T) {
	cases := []struct{ href, want string }{
		{"/kiosk", "nav.kiosk"},
		{"/admin/dashboard", "nav.admin.dashboard"},
		{"/", "nav.home"},
		{"", "nav.home"},
		{"/Notes/Board 1", "nav.notes.board-1"},
	}
	for _, tc := range cases {
		if got := NavEventName(tc.href); got != tc.want {
			t.Errorf("NavEventName(%q) = %q, want %q", tc.href, got, tc.want)
		}
	}
}

func TestWithAnalyticsSeedsTheFlag(t *testing.T) {
	if Analytics(context.Background()) {
		t.Fatal("a bare context reported analytics on")
	}
	if !Analytics(WithAnalytics(context.Background(), true)) {
		t.Fatal("WithAnalytics(true) did not seed the flag")
	}
}

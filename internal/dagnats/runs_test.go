package dagnats

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestListRunsParsesArray(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/runs" {
			t.Errorf("path = %q, want /runs", r.URL.Path)
		}
		if got := r.URL.Query().Get("workflow"); got != "onboarding" {
			t.Errorf("workflow = %q, want onboarding", got)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`[
			{"run_id":"r1","workflow_id":"onboarding","status":"running","created_at":"2026-11-07T10:00:00Z",
			 "steps":{"greet":{"status":"completed","attempts":1},"await-first-todo":{"status":"running","attempts":1}}}
		]`))
	}))
	defer srv.Close()

	c := NewClient(srv.URL)
	runs, err := c.ListRuns(context.Background(), "onboarding", 5)
	if err != nil {
		t.Fatalf("ListRuns: %v", err)
	}
	if len(runs) != 1 || runs[0].RunID != "r1" {
		t.Fatalf("runs = %+v, want one r1", runs)
	}
	if runs[0].Steps["await-first-todo"].Status != "running" {
		t.Fatalf("step status = %q, want running", runs[0].Steps["await-first-todo"].Status)
	}
}

func TestListRunsSurfacesEngineError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		http.Error(w, "nope", http.StatusInternalServerError)
	}))
	defer srv.Close()

	_, err := NewClient(srv.URL).ListRuns(context.Background(), "", 0)
	if err == nil {
		t.Fatal("ListRuns with 500 = nil, want error")
	}
}

package installer

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestLatestTagParses(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !strings.HasSuffix(r.URL.Path, "/releases/latest") {
			t.Errorf("unexpected path %s", r.URL.Path)
		}
		_, _ = w.Write([]byte(`{"tag_name":"v9.9.9"}`))
	}))
	defer srv.Close()
	tag, err := latestTag(context.Background(), srv.Client(), srv.URL, "o", "r")
	if err != nil {
		t.Fatalf("latestTag: %v", err)
	}
	if tag != "v9.9.9" {
		t.Errorf("tag = %q, want v9.9.9", tag)
	}
}

func TestLatestTagFailsFast(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusNotFound)
	}))
	defer srv.Close()
	if _, err := latestTag(context.Background(), srv.Client(), srv.URL, "o", "r"); err == nil {
		t.Error("non-200 must fail fast")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()
	if _, err := latestTag(ctx, nil, "http://127.0.0.1:1", "o", "r"); err == nil {
		t.Error("unreachable API must fail fast")
	}
}

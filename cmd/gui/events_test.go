// SCOPE:feature — native window event-stream client tests (headless).
package main

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/calionauta/gogogo/config"
	"github.com/calionauta/gogogo/internal/auth"
)

// TestWatchEventsDeliversFrames pins the native client's half of the JSON
// stream: the session token rides the cookie, comments are ignored, and every
// data frame triggers a re-list.
//
// RED-PROOFED: drop the onEvent call and the test times out; send the token in
// the wrong cookie and the stub answers 401 (the watcher reports not
// connected).
func TestWatchEventsDeliversFrames(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != eventsPath {
			t.Errorf("path = %q, want %q", r.URL.Path, eventsPath)
			http.NotFound(w, r)
			return
		}
		cookie, err := r.Cookie(auth.CookieName)
		if err != nil || cookie.Value != "tok-1" {
			t.Errorf("session cookie = %v (err %v), want tok-1", cookie, err)
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		flusher, ok := w.(http.Flusher)
		if !ok {
			t.Error("test writer cannot flush")
			return
		}
		w.Header().Set("Content-Type", "text/event-stream")
		w.WriteHeader(http.StatusOK)
		_, _ = fmt.Fprint(w, ": connected\n\n")
		flusher.Flush()
		for i := 0; i < 2; i++ {
			_, _ = fmt.Fprintf(w, "data: {\"n\":%d}\n\n", i)
			flusher.Flush()
		}
		<-r.Context().Done()
	}))
	defer srv.Close()

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	frames := make(chan struct{}, 4)
	done := make(chan error, 1)
	go func() {
		_, err := watchEvents(ctx, srv.URL, "tok-1", func() { frames <- struct{}{} })
		done <- err
	}()

	for i := 0; i < 2; i++ {
		select {
		case <-frames:
		case err := <-done:
			t.Fatalf("watcher ended before frame %d: %v", i, err)
		case <-time.After(3 * time.Second):
			t.Fatalf("frame %d never delivered", i)
		}
	}
}

// TestEventBaseURL pins the dial target: a wildcard bind is not a valid
// destination, so the window talks to loopback instead.
func TestEventBaseURL(t *testing.T) {
	cases := []struct {
		host string
		port int
		want string
	}{
		{"0.0.0.0", 8080, "http://127.0.0.1:8080"},
		{"", 9090, "http://127.0.0.1:9090"},
		{"::", 8080, "http://127.0.0.1:8080"},
		{"127.0.0.1", 8080, "http://127.0.0.1:8080"},
		{"10.0.0.5", 3000, "http://10.0.0.5:3000"},
	}
	for _, c := range cases {
		if got := eventBaseURL(&config.Config{Host: c.host, Port: c.port}); got != c.want {
			t.Errorf("eventBaseURL(%q,%d) = %q, want %q", c.host, c.port, got, c.want)
		}
	}
}

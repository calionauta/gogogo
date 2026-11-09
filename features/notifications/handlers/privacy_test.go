// Inbox owner privacy.
package handlers

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/calionauta/gogogo/internal/auth"
)

// TestInboxListsOnlyCallersOwnRows pins the privacy contract the user asked
// about: a reminder the author scheduled lands only in the author's inbox —
// another signed-in user must not see its subject or body anywhere.
func TestInboxListsOnlyCallersOwnRows(t *testing.T) {
	h, q, _ := newFixture(t)
	ada := createUser(t, q, "ada@demo.app")
	bob := createUser(t, q, "bob@demo.app")

	// Deliver through the same seam the reminder workflow uses (Owner = the
	// author's own id, never from the request).
	deliver := func(owner, tx, subject, body string) {
		t.Helper()
		if err := h.Deliver(context.Background(), Delivery{
			Owner: owner, TransactionID: tx, Workflow: "reminder",
			Subject: subject, Body: body,
		}); err != nil {
			t.Fatalf("deliver: %v", err)
		}
	}
	deliver(ada.ID, "reminder:ada-only", "AdaSecretReminder", "Only Ada sees this body")
	deliver(bob.ID, "reminder:bob-only", "BobSecretReminder", "Bob sees this body")

	serve := func(t *testing.T, h *Handler, uid string) (*httptest.Server, *http.Cookie) {
		t.Helper()
		mux := http.NewServeMux()
		mux.Handle("/", h.Routes())
		srv := httptest.NewServer(mux)
		t.Cleanup(srv.Close)
		tok, err := h.auth.OpenSession(context.Background(), uid)
		if err != nil {
			t.Fatalf("open session: %v", err)
		}
		// #nosec G124 — test-only request cookie; never traverses a network.
		return srv, &http.Cookie{Name: auth.CookieName, Value: tok}
	}
	serverA, cookieA := serve(t, h, ada.ID)
	serverB, cookieB := serve(t, h, bob.ID)

	get := func(t *testing.T, server *httptest.Server, cookie *http.Cookie) string {
		t.Helper()
		req, err := http.NewRequestWithContext(context.Background(), http.MethodGet,
			server.URL+"/notifications", nil)
		if err != nil {
			t.Fatalf("build GET inbox: %v", err)
		}
		req.AddCookie(cookie)
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatalf("GET inbox: %v", err)
		}
		defer resp.Body.Close()
		raw, err := io.ReadAll(resp.Body)
		if err != nil {
			t.Fatalf("read inbox: %v", err)
		}
		return string(raw)
	}

	// Bob sees his own row, never Ada's (the leak the user asked about).
	gotB := get(t, serverB, cookieB)
	if !strings.Contains(gotB, "BobSecretReminder") {
		t.Fatal("Bob's own reminder is missing from his inbox")
	}
	if strings.Contains(gotB, "AdaSecretReminder") || strings.Contains(gotB, "Only Ada sees this body") {
		t.Fatal("Bob's inbox leaks Ada's reminder")
	}

	// Symmetric: Ada sees hers, never Bob's.
	gotA := get(t, serverA, cookieA)
	if !strings.Contains(gotA, "AdaSecretReminder") {
		t.Fatal("Ada's own reminder is missing from her inbox")
	}
	if strings.Contains(gotA, "BobSecretReminder") {
		t.Fatal("Ada's inbox leaks Bob's reminder")
	}
}

package productapi

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// serveProduct stands up the two routes the client talks to, backed by a fixed
// answer. The client is written against the wire shape, not against the
// server package — that separation is the whole point — so this fixture pins
// the contract both sides must agree on.
func serveProduct(t *testing.T, units []Unit) *httptest.Server {
	t.Helper()
	mux := http.NewServeMux()
	mux.HandleFunc("/api/ext/descriptors", func(w http.ResponseWriter, r *http.Request) {
		if _, err := r.Cookie(SessionCookie); err != nil {
			http.Error(w, "unauthorized", http.StatusUnauthorized)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(units)
	})
	mux.HandleFunc("/api/ext/invoke", func(w http.ResponseWriter, r *http.Request) {
		if _, err := r.Cookie(SessionCookie); err != nil {
			http.Error(w, "unauthorized", http.StatusUnauthorized)
			return
		}
		var req struct {
			ID     string            `json:"id"`
			Verb   string            `json:"verb"`
			Params map[string]string `json:"params"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			http.Error(w, "bad request", http.StatusBadRequest)
			return
		}
		if req.ID != "probe" || req.Verb != "read" {
			http.Error(w, "no such verb", http.StatusNotFound)
			return
		}
		_, _ = w.Write([]byte("summary: 3 open"))
	})
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	return srv
}

func probeUnits() []Unit {
	return []Unit{{
		ID: "probe",
		Verbs: []Verb{
			{Verb: "read", Method: "GET", Pattern: "/probe/read", Query: []string{"scope"}},
			{Verb: "create", Method: "POST", Pattern: "/probe", Params: []string{"title"}, RawBody: "body"},
		},
	}}
}

// The happy path: discover, then call. This is the whole consumer story.
func TestClientDescribesThenInvokes(t *testing.T) {
	srv := serveProduct(t, probeUnits())
	c := Client{BaseURL: srv.URL, Session: "sess"}
	ctx := t.Context()

	units, err := c.Descriptors(ctx)
	if err != nil {
		t.Fatalf("describe: %v", err)
	}
	if len(units) != 1 || units[0].ID != "probe" {
		t.Fatalf("units = %+v", units)
	}

	got, err := c.Invoke(ctx, "probe", "read", nil)
	if err != nil {
		t.Fatalf("invoke: %v", err)
	}
	if !strings.Contains(got, "3 open") {
		t.Errorf("body = %q", got)
	}
}

// A typo must name the alternative. An agent that gets "400" without the list
// has to guess, and guessing is how a caller ends up calling the wrong verb.
func TestInvokeNamesTheVerbsThatExist(t *testing.T) {
	srv := serveProduct(t, probeUnits())
	c := Client{BaseURL: srv.URL, Session: "sess"}

	_, err := c.Invoke(t.Context(), "probe", "reed", nil)
	if err == nil {
		t.Fatal("expected an error for an unknown verb")
	}
	for _, want := range []string{"reed", "create", "read"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("error %q does not mention %q", err, want)
		}
	}
}

// Where a param travels is the contract. Sending a query param in the body
// reaches a handler that sees nothing, so the client refuses it with the reason.
func TestInvokeRefusesAQueryParamInTheBody(t *testing.T) {
	srv := serveProduct(t, probeUnits())
	c := Client{BaseURL: srv.URL, Session: "sess"}

	_, err := c.Invoke(t.Context(), "probe", "read", map[string]string{"scope": "week"})
	if err == nil {
		t.Fatal("expected an error for a query param sent as a body field")
	}
	if !strings.Contains(err.Error(), "query") {
		t.Errorf("error %q does not explain that the param travels in the URL", err)
	}
}

// The client presents a session, never a password, and refuses to start without
// one instead of dialing and discovering a 401.
func TestClientRequiresASessionBeforeDialing(t *testing.T) {
	c := Client{BaseURL: "http://127.0.0.1:1"}
	if _, err := c.Descriptors(t.Context()); err == nil ||
		!strings.Contains(err.Error(), "no session") {
		t.Fatalf("err = %v, want a no-session error", err)
	}
}

// Loopback containment: a remote product is reached through SSH or Tailscale,
// not by pointing this client at a public URL.
func TestClientRefusesANonLoopbackServer(t *testing.T) {
	c := Client{BaseURL: "https://example.com", Session: "sess"}
	_, err := c.Descriptors(t.Context())
	if err == nil || !strings.Contains(err.Error(), "not loopback") {
		t.Fatalf("err = %v, want a loopback refusal", err)
	}
}

// An expired session is a 401, and the error says so rather than surfacing as a
// decode failure.
func TestClientSurfacesAnExpiredSession(t *testing.T) {
	srv := serveProduct(t, probeUnits())
	mux := newBadSessionServer(t)
	c := Client{BaseURL: mux.URL, Session: "expired"}
	_, err := c.Descriptors(t.Context())
	_ = srv
	if err == nil || !strings.Contains(err.Error(), "401") {
		t.Fatalf("err = %v, want a 401", err)
	}
}

func newBadSessionServer(t *testing.T) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
	}))
	t.Cleanup(srv.Close)
	return srv
}

// TestBothDoorsApplyTheSameLoopbackRule pins the convergence.
//
// ValidateBaseURL (a caller checking a URL up front) and Client.validate (the
// dial itself) each carried their own hand-written host check, with different
// host sets: one accepted only "localhost", the other also 127.0.0.1 and ::1.
// So the same address was valid at one door and refused at the other, and the
// gap showed up only as a confusing failure after a "successful" validation.
func TestBothDoorsApplyTheSameLoopbackRule(t *testing.T) {
	for _, raw := range []string{
		"http://localhost:8080",
		"http://127.0.0.1:8080",
		"http://[::1]:8080",
		"https://app.localhost",
	} {
		if _, err := ValidateBaseURL(raw); err != nil {
			t.Errorf("ValidateBaseURL(%q) = %v, want accepted", raw, err)
		}
		if _, err := New(raw, "sess").validate(); err != nil {
			t.Errorf("validate(%q) = %v, want accepted", raw, err)
		}
	}
	for _, raw := range []string{
		"http://example.com",
		"http://10.0.0.5:8080",
		"ftp://localhost",
	} {
		if _, err := ValidateBaseURL(raw); err == nil {
			t.Errorf("ValidateBaseURL(%q) accepted a non-loopback address", raw)
		}
		if _, err := New(raw, "sess").validate(); err == nil {
			t.Errorf("validate(%q) accepted a non-loopback address", raw)
		}
	}
}

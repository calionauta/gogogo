package server

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"

	"github.com/calionauta/gogogo/config"
	"github.com/calionauta/gogogo/internal/events"
)

// eventRecorder adapts a function to events.Sink for the assertions below.
type eventRecorder func(events.Event)

func (f eventRecorder) Deliver(_ context.Context, event events.Event) { f(event) }

func TestEmitInteractionPublishesWritesOnly(t *testing.T) {
	bus := events.New(4)
	bus.Start()
	t.Cleanup(bus.Close)
	delivered := make(chan events.Event, 4)
	bus.AddSink(eventRecorder(func(event events.Event) { delivered <- event }))

	handler := emitInteraction(bus, "todo", http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusCreated)
	}))
	handler.ServeHTTP(httptest.NewRecorder(),
		httptest.NewRequestWithContext(t.Context(), http.MethodPost, "/api/todos", nil))

	select {
	case event := <-delivered:
		if event.Name != eventCapabilityInteraction {
			t.Errorf("event name = %q, want %q", event.Name, eventCapabilityInteraction)
		}
		if event.Fields["capability"] != "todo" {
			t.Errorf("capability = %v, want todo", event.Fields["capability"])
		}
		if event.Fields["status"] != http.StatusCreated {
			t.Errorf("status = %v, want %d", event.Fields["status"], http.StatusCreated)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("a mutating request emitted no event")
	}

	handler.ServeHTTP(httptest.NewRecorder(),
		httptest.NewRequestWithContext(t.Context(), http.MethodGet, "/todo", nil))
	select {
	case event := <-delivered:
		t.Fatalf("a read emitted %q; the bus carries writes only", event.Name)
	case <-time.After(50 * time.Millisecond):
		// The absence is the assertion.
	}
}

func TestMountAnalyticsOnlyWhenAProviderIsConfigured(t *testing.T) {
	bus := events.New(1)
	t.Cleanup(bus.Close)

	on, off := NewRouter(), NewRouter()
	enabled := &config.Config{}
	enabled.Events.AnalyticsProvider = "posthog"
	enabled.Events.AnalyticsKey = "phc_test"
	mountAnalytics(on, nil, enabled, bus)
	if !on.serves(http.MethodPost, analyticsPath) {
		t.Error("analytics route missing with a provider configured")
	}
	mountAnalytics(off, nil, &config.Config{}, bus)
	if off.serves(http.MethodPost, analyticsPath) {
		t.Error("analytics route mounted with no provider — a dead beacon surface")
	}
}

func TestCaptureUIEventValidatesAndAttributes(t *testing.T) {
	bus := events.New(4)
	bus.Start()
	t.Cleanup(bus.Close)
	delivered := make(chan events.Event, 2)
	bus.AddSink(eventRecorder(func(event events.Event) { delivered <- event }))

	if err := captureUIEvent(t.Context(), bus, "user-1", []byte(`{"name":"nav.todo","path":"/todo"}`)); err != nil {
		t.Fatalf("valid beacon rejected: %v", err)
	}
	select {
	case event := <-delivered:
		if event.Name != "ui.nav.todo" {
			t.Errorf("event name = %q, want ui.nav.todo", event.Name)
		}
		if event.Fields["actor"] != "user-1" {
			t.Errorf("actor = %v, want the session owner", event.Fields["actor"])
		}
	case <-time.After(2 * time.Second):
		t.Fatal("valid beacon emitted no event")
	}

	if err := captureUIEvent(t.Context(), bus, "user-1", []byte(`{"name":"Not Allowed!"}`)); err == nil {
		t.Error("an off-allowlist name was accepted")
	}
	if err := captureUIEvent(t.Context(), bus, "user-1", []byte(`not json`)); err == nil {
		t.Error("a malformed beacon was accepted")
	}
}

// TestAPIShapeMountSkipsPageRoutes pins the shape at the mount: the page route
// never reaches the tree, so its handler and RBAC read cannot run. Red-proof:
// drop the `pageRoutes` check in MountFeature and /todo is claimed again.
func TestAPIShapeMountSkipsPageRoutes(t *testing.T) {
	ok := func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusOK) }
	build := func(r *Router) {
		feature := chi.NewRouter()
		feature.Get("/todo", ok)
		feature.Post("/api/todos", ok)
		r.MountFeature("todo", feature)
	}

	api := NewRouter()
	api.pageRoutes = false
	build(api)
	if api.serves(http.MethodGet, "/todo") {
		t.Error("the api shape mounted a page route")
	}
	if !api.serves(http.MethodPost, "/api/todos") {
		t.Error("the api shape dropped an API route")
	}

	full := NewRouter()
	build(full)
	if !full.serves(http.MethodGet, "/todo") {
		t.Error("the full shape dropped a page route")
	}
}

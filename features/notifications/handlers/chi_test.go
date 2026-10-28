// SCOPE:feature — notifications handler contract.
package handlers

import (
	"context"
	"database/sql"
	"io"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/calionauta/gogogo/config"
	"github.com/calionauta/gogogo/internal/auth"
	dbpkg "github.com/calionauta/gogogo/internal/db"
	dbgen "github.com/calionauta/gogogo/internal/db/sqlcgen"
	"github.com/calionauta/gogogo/internal/dbschema"

	"github.com/calionauta/gogogo/features/notifications"
	"github.com/calionauta/gogogo/internal/delivery"
)

// newFixture builds a migrated temp database, a real auth stack, the bus, and
// the handler. The handler has no run starter: the tests that schedule inject
// a stub, and the ones that read stay on the read path.
func newFixture(t *testing.T) (*Handler, *dbgen.Queries, *notifications.Bus) {
	t.Helper()
	h, q, bus, _ := newFixtureWith(t, delivery.NewRouter(nil))
	return h, q, bus
}

// newFixtureWith is the same fixture with a chosen transport, returning the
// database handle so a test can read the rows the inbox list hides (email).
func newFixtureWith(
	t *testing.T, router *delivery.Router,
) (*Handler, *dbgen.Queries, *notifications.Bus, *sql.DB) {
	t.Helper()
	sqlDB, err := dbpkg.OpenAt(filepath.Join(t.TempDir(), "notifications.db"))
	if err != nil {
		t.Fatalf("db open: %v", err)
	}
	t.Cleanup(func() { _ = sqlDB.Close() })
	if merr := dbschema.Migrate(sqlDB); merr != nil {
		t.Fatalf("db migrate: %v", merr)
	}
	q := dbgen.New(sqlDB)
	a, aerr := auth.New(q, false)
	if aerr != nil {
		t.Fatalf("auth new: %v", aerr)
	}
	bus := notifications.NewBus()
	return NewHandler(a, &config.Config{AppName: "gogogo"}, q, bus, router), q, bus, sqlDB
}

func createUser(t *testing.T, q *dbgen.Queries, email string) dbgen.User {
	t.Helper()
	now := dbschema.Now()
	u, err := q.CreateUser(context.Background(), dbgen.CreateUserParams{
		ID:           dbschema.NewID(),
		Email:        email,
		PasswordHash: "hash",
		Name:         "Demo",
		Created:      now,
		Updated:      now,
	})
	if err != nil {
		t.Fatalf("create user: %v", err)
	}
	return u
}

// TestReminderDueAt pins the delay mapping, including the refusal to guess.
// An unknown delay must be rejected, never defaulted: a reminder that fires at
// a time the user did not pick is worse than a 400.
func TestReminderDueAt(t *testing.T) {
	now := time.Date(2026, 10, 27, 12, 0, 0, 0, time.UTC)
	cases := []struct {
		delay string
		want  string
		ok    bool
	}{
		{delay: "1m", want: "2026-10-27T12:01:00Z", ok: true},
		{delay: "1h", want: "2026-10-27T13:00:00Z", ok: true},
		{delay: "24h", want: "2026-10-28T12:00:00Z", ok: true},
		{delay: "", want: "", ok: false},
		{delay: "2m", want: "", ok: false},
	}
	for _, tc := range cases {
		got, ok := reminderDueAt(tc.delay, now)
		if ok != tc.ok || got != tc.want {
			t.Errorf("reminderDueAt(%q) = (%q, %v), want (%q, %v)", tc.delay, got, ok, tc.want, tc.ok)
		}
	}
}

// TestDeliverIsIdempotentAndPublishesOnce pins two contracts the engine and
// the live inbox rely on: the same transaction delivered twice writes one row
// and reports success both times, and only the write that actually happened
// wakes the owner's streams (a retry must not make a tab flicker).
func TestDeliverIsIdempotentAndPublishesOnce(t *testing.T) {
	h, q, bus := newFixture(t)
	owner := createUser(t, q, "ada@demo.app").ID

	var events int
	cancel := bus.Subscribe(func(ev notifications.Event) {
		if ev.Owner == owner {
			events++
		}
	})
	defer cancel()

	d := Delivery{
		Owner:         owner,
		TransactionID: "reminder:1",
		Workflow:      "reminder",
		Subject:       "Reminder",
		Body:          "Drink water",
	}
	if err := h.Deliver(context.Background(), d); err != nil {
		t.Fatalf("first deliver: %v", err)
	}
	if err := h.Deliver(context.Background(), d); err != nil {
		t.Fatalf("retried deliver must be a no-op, got %v", err)
	}

	rows, err := q.ListNotificationsForOwner(context.Background(),
		dbgen.ListNotificationsForOwnerParams{Owner: owner, Limit: 10})
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if len(rows) != 1 {
		t.Fatalf("rows = %d, want 1 (the retry must not duplicate)", len(rows))
	}
	if rows[0].Channel != "in_app" || rows[0].Status != "sent" || rows[0].Body != "Drink water" {
		t.Fatalf("row = %+v, want channel=in_app status=sent body preserved", rows[0])
	}
	if events != 1 {
		t.Fatalf("published events = %d, want 1 (the duplicate must not publish)", events)
	}
}

// TestBusDeliversEveryEventAndCancelStops pins the bus contract the stream
// relies on: publish reaches every current subscriber, and a cancelled
// subscription receives nothing more.
func TestBusDeliversEveryEventAndCancelStops(t *testing.T) {
	bus := notifications.NewBus()
	got := make(chan notifications.Event, 4)
	cancel := bus.Subscribe(func(ev notifications.Event) { got <- ev })

	bus.Publish(notifications.Event{Owner: "ada"})
	bus.Publish(notifications.Event{Owner: "bob"})
	for _, want := range []string{"ada", "bob"} {
		select {
		case ev := <-got:
			if ev.Owner != want {
				t.Fatalf("event owner = %q, want %q", ev.Owner, want)
			}
		case <-time.After(time.Second):
			t.Fatalf("no event for %q", want)
		}
	}

	cancel()
	bus.Publish(notifications.Event{Owner: "ada"})
	select {
	case ev := <-got:
		t.Fatalf("cancelled subscription received %+v", ev)
	case <-time.After(50 * time.Millisecond):
	}
}

// TestWakeForFiltersOwnerAndExcludedTab pins the stream's routing rule: an
// event wakes the owner's tabs, never another owner's, and never the tab whose
// own request already rendered the change.
func TestWakeForFiltersOwnerAndExcludedTab(t *testing.T) {
	cases := []struct {
		name     string
		ev       notifications.Event
		owner    string
		clientID string
		want     bool
	}{
		{
			name: "same owner", ev: notifications.Event{Owner: "ada"},
			owner: "ada", clientID: "tab-1", want: true,
		},
		{
			name: "other owner", ev: notifications.Event{Owner: "bob"},
			owner: "ada", clientID: "tab-1", want: false,
		},
		{
			name: "origin tab excluded", ev: notifications.Event{Owner: "ada", Except: "tab-1"},
			owner: "ada", clientID: "tab-1", want: false,
		},
		{
			name: "other tab of same owner", ev: notifications.Event{Owner: "ada", Except: "tab-2"},
			owner: "ada", clientID: "tab-1", want: true,
		},
	}
	for _, tc := range cases {
		if got := wakeFor(tc.ev, tc.owner, tc.clientID); got != tc.want {
			t.Errorf("%s: wakeFor = %v, want %v", tc.name, got, tc.want)
		}
	}
}

// TestAnonymousRequestsAreRefused pins that the slice guards itself: RBAC can
// be disabled, so a policy row is never the only check.
func TestAnonymousRequestsAreRefused(t *testing.T) {
	h, _, _ := newFixture(t)
	router := h.Routes()

	cases := []struct {
		method string
		path   string
		want   int
	}{
		{method: http.MethodPost, path: "/api/notifications/read-all", want: http.StatusUnauthorized},
		{method: http.MethodPost, path: "/api/notifications/abc/read", want: http.StatusUnauthorized},
		{method: http.MethodPost, path: "/api/notifications/remind", want: http.StatusUnauthorized},
		{method: http.MethodGet, path: "/api/notifications/stream", want: http.StatusUnauthorized},
		{method: http.MethodGet, path: "/api/notifications/badge", want: http.StatusUnauthorized},
		{method: http.MethodGet, path: "/notifications", want: http.StatusSeeOther},
	}
	for _, tc := range cases {
		rec := httptest.NewRecorder()
		router.ServeHTTP(rec, httptest.NewRequestWithContext(context.Background(), tc.method, tc.path, nil))
		if rec.Code != tc.want {
			t.Errorf("%s %s = %d, want %d", tc.method, tc.path, rec.Code, tc.want)
		}
		if tc.want == http.StatusSeeOther && !strings.Contains(rec.Header().Get("Location"), "/login") {
			t.Errorf("%s location = %q, want a /login redirect", tc.path, rec.Header().Get("Location"))
		}
	}
}

// fakeSender records calls so a test can prove the provider was (or was not)
// used, without a network.
type fakeSender struct {
	calls   int
	last    delivery.Message
	receipt delivery.Receipt
	err     error
}

func (f *fakeSender) Name() string { return "fake" }

func (f *fakeSender) Send(_ context.Context, m delivery.Message) (delivery.Receipt, error) {
	f.calls++
	f.last = m
	return f.receipt, f.err
}

// TestDeliverSendsEmailThroughTheProvider proves the transport seam: the same
// Delivery that writes the inbox row reaches the configured Sender once, is
// recorded with the provider's name, and a retried run does not send twice.
func TestDeliverSendsEmailThroughTheProvider(t *testing.T) {
	sender := &fakeSender{receipt: delivery.Receipt{Provider: "fake", Status: delivery.StatusSent}}
	router := delivery.NewRouter(map[delivery.Channel]delivery.Sender{delivery.ChannelEmail: sender})
	h, q, _, sqlDB := newFixtureWith(t, router)
	owner := createUser(t, q, "ada@demo.app").ID

	d := Delivery{
		Owner: owner, TransactionID: "txn-email", Workflow: "reminder",
		Subject: "Prazo", Body: "Vence hoje",
		Channels:   []delivery.Channel{delivery.ChannelEmail},
		Recipients: map[delivery.Channel]string{delivery.ChannelEmail: "ada@demo.app"},
	}
	if err := h.Deliver(context.Background(), d); err != nil {
		t.Fatalf("first deliver: %v", err)
	}
	if err := h.Deliver(context.Background(), d); err != nil {
		t.Fatalf("retried deliver: %v", err)
	}
	if sender.calls != 1 {
		t.Fatalf("provider calls = %d, want 1 (the retry must not resend)", sender.calls)
	}
	// Per-channel transaction id: a provider that dedupes on transaction+recipient
	// would drop the second channel of one delivery otherwise.
	if sender.last.TransactionID != "txn-email:email" {
		t.Fatalf("provider transaction = %q, want txn-email:email", sender.last.TransactionID)
	}

	var status, provider, skipReason string
	if err := sqlDB.QueryRowContext(context.Background(),
		`SELECT status, provider, skip_reason FROM notifications WHERE owner = ? AND channel = 'email'`,
		owner).Scan(&status, &provider, &skipReason); err != nil {
		t.Fatalf("read email row: %v", err)
	}
	if status != delivery.StatusSent || provider != "fake" || skipReason != "" {
		t.Fatalf("email row = (%q, %q, %q), want (sent, fake, \"\")", status, provider, skipReason)
	}

	// The inbox list stays in_app-only: one row, not the email copy.
	rows, err := q.ListNotificationsForOwner(context.Background(),
		dbgen.ListNotificationsForOwnerParams{Owner: owner, Limit: 10})
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if len(rows) != 1 || rows[0].Channel != "in_app" {
		t.Fatalf("inbox rows = %+v, want exactly one in_app row", rows)
	}
}

// TestDeliverRecordsSkippedWhenNoProvider pins the honest no-transport path:
// asking for email without a provider writes skipped/provider instead of
// dropping the request.
func TestDeliverRecordsSkippedWhenNoProvider(t *testing.T) {
	h, q, _, sqlDB := newFixtureWith(t, delivery.NewRouter(nil))
	owner := createUser(t, q, "bob@demo.app").ID

	if err := h.Deliver(context.Background(), Delivery{
		Owner: owner, TransactionID: "txn-skip", Workflow: "reminder",
		Subject: "S", Body: "B",
		Channels:   []delivery.Channel{delivery.ChannelEmail},
		Recipients: map[delivery.Channel]string{delivery.ChannelEmail: "bob@demo.app"},
	}); err != nil {
		t.Fatalf("deliver: %v", err)
	}

	var status, skipReason string
	if err := sqlDB.QueryRowContext(context.Background(),
		`SELECT status, skip_reason FROM notifications WHERE owner = ? AND channel = 'email'`,
		owner).Scan(&status, &skipReason); err != nil {
		t.Fatalf("read email row: %v", err)
	}
	if status != delivery.StatusSkipped || skipReason != "provider" {
		t.Fatalf("email row = (%q, %q), want (skipped, provider)", status, skipReason)
	}
}

// TestDeliverChannelGuards pins the channel model's two refusals: in_app in the
// requested list is ignored (the inbox row is written once, above the loop),
// and a channel with no address is recorded skipped/no_address without ever
// touching the provider.
func TestDeliverChannelGuards(t *testing.T) {
	sender := &fakeSender{receipt: delivery.Receipt{Provider: "fake", Status: delivery.StatusSent}}
	router := delivery.NewRouter(map[delivery.Channel]delivery.Sender{
		delivery.ChannelEmail:   sender,
		delivery.Channel("sms"): sender,
	})
	h, q, _, sqlDB := newFixtureWith(t, router)
	owner := createUser(t, q, "guard@demo.app").ID

	if err := h.Deliver(context.Background(), Delivery{
		Owner: owner, TransactionID: "txn-guard", Workflow: "reminder",
		Subject: "S", Body: "B",
		Channels:   []delivery.Channel{delivery.ChannelInApp, delivery.Channel("sms")},
		Recipients: map[delivery.Channel]string{delivery.Channel("sms"): ""},
	}); err != nil {
		t.Fatalf("deliver: %v", err)
	}
	if sender.calls != 0 {
		t.Fatalf("provider calls = %d, want 0 (no channel was deliverable)", sender.calls)
	}

	var inApp, sms int
	if err := sqlDB.QueryRowContext(context.Background(),
		`SELECT count(*) FROM notifications WHERE owner = ? AND channel = 'in_app'`, owner).Scan(&inApp); err != nil {
		t.Fatalf("count in_app: %v", err)
	}
	if inApp != 1 {
		t.Fatalf("in_app rows = %d, want 1 (in_app must not be sent as a transport)", inApp)
	}
	const smsSkip = `SELECT count(*) FROM notifications
		WHERE owner = ? AND channel = 'sms' AND status = 'skipped' AND skip_reason = 'no_address'`
	if err := sqlDB.QueryRowContext(context.Background(), smsSkip, owner).Scan(&sms); err != nil {
		t.Fatalf("count sms: %v", err)
	}
	if sms != 1 {
		t.Fatalf("sms skipped/no_address rows = %d, want 1", sms)
	}
}

// stubRunStarter accepts every schedule; the reminder handler only needs the
// start to succeed, because the durable engine owns the wait.
type stubRunStarter struct{}

func (stubRunStarter) StartRun(context.Context, string, any) (string, error) {
	return "run-test", nil
}

// signedInServer wires the real auth routes so a test can obtain a session
// cookie the same way a browser does; the handler's own routes are mounted at
// the root.
func signedInServer(t *testing.T, h *Handler, email string) (*httptest.Server, *http.Cookie) {
	t.Helper()
	mux := http.NewServeMux()
	mux.Handle("/api/auth/", h.auth.Routes())
	mux.Handle("/", h.Routes())
	server := httptest.NewServer(mux)
	t.Cleanup(server.Close)

	body := `{"email":"` + email + `","password":"[LEAKGUARD_REDACTED] — Password Assignment"}`
	req, err := http.NewRequestWithContext(context.Background(), http.MethodPost,
		server.URL+"/api/auth/signup/credential", strings.NewReader(body))
	if err != nil {
		t.Fatalf("build signup: %v", err)
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("signup: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("signup status = %d", resp.StatusCode)
	}
	for _, c := range resp.Cookies() {
		if c.Name == auth.CookieName {
			return server, c
		}
	}
	t.Fatal("signup returned no session cookie")
	return nil, nil
}

// postForm sends one authenticated form POST and returns the SSE transcript.
func postForm(t *testing.T, server *httptest.Server, cookie *http.Cookie, path, form string) string {
	t.Helper()
	req, err := http.NewRequestWithContext(context.Background(), http.MethodPost,
		server.URL+path, strings.NewReader(form))
	if err != nil {
		t.Fatalf("build POST %s: %v", path, err)
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.AddCookie(cookie)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("POST %s: %v", path, err)
	}
	defer resp.Body.Close()
	raw, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("POST %s status = %d (%s)", path, resp.StatusCode, raw)
	}
	return string(raw)
}

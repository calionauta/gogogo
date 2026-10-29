// SCOPE:feature — Inbox page surfaces at the handler level: the State panel's
// accounting and the shell patches a read/schedule writes outside #inbox.
package handlers

import (
	"context"
	"io"
	"net/http"
	"strings"
	"testing"

	dbgen "github.com/calionauta/gogogo/internal/db/sqlcgen"
	"github.com/calionauta/gogogo/internal/dbschema"

	"github.com/calionauta/gogogo/internal/delivery"
)

// TestInboxStatePanelCountsEveryChannel pins the panel's accounting end to end:
// the list is in_app-only by design, so the email row never appears there, and
// the State panel is the surface that shows it (sent/skipped is the delivery
// record, not a second copy of the message).
func TestInboxStatePanelCountsEveryChannel(t *testing.T) {
	h, q, _, _ := newFixtureWith(t, delivery.NewRouter(nil))
	server, cookie := signedInServer(t, h, "panel@demo.app")
	u, err := q.GetUserByEmail(context.Background(), "panel@demo.app")
	if err != nil {
		t.Fatalf("get user: %v", err)
	}
	if err := h.Deliver(context.Background(), Delivery{
		Owner: u.ID, TransactionID: "panel:1", Workflow: "reminder",
		Subject: "S", Body: "B",
		Channels:   []delivery.Channel{delivery.ChannelEmail},
		Recipients: map[delivery.Channel]string{delivery.ChannelEmail: "panel@demo.app"},
	}); err != nil {
		t.Fatalf("deliver: %v", err)
	}
	// Another owner's rows must not reach this page: the count query is
	// owner-scoped, and the panel would otherwise aggregate every account's
	// deliveries into one bucket.
	other := createUser(t, q, "other@demo.app").ID
	for _, txn := range []string{"other:1", "other:2", "other:3"} {
		if _, cerr := q.CreateNotification(context.Background(), dbgen.CreateNotificationParams{
			ID: dbschema.NewID(), Owner: other, Workflow: "reminder",
			TransactionID: txn, Channel: "email", Status: delivery.StatusSent,
			Provider: "fake", Subject: "S", Body: "B", Created: dbschema.Now(),
		}); cerr != nil {
			t.Fatalf("create other row: %v", cerr)
		}
	}

	req, rerr := http.NewRequestWithContext(context.Background(), http.MethodGet,
		server.URL+"/notifications", nil)
	if rerr != nil {
		t.Fatalf("build GET: %v", rerr)
	}
	req.AddCookie(cookie)
	resp, gerr := http.DefaultClient.Do(req)
	if gerr != nil {
		t.Fatalf("GET /notifications: %v", gerr)
	}
	defer resp.Body.Close()
	raw, berr := io.ReadAll(resp.Body)
	if berr != nil {
		t.Fatalf("read page: %v", berr)
	}
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("GET /notifications status = %d", resp.StatusCode)
	}
	html := string(raw)
	for _, want := range []string{"Delivery pipeline", "In-app · sent", "Email · skipped"} {
		if !strings.Contains(html, want) {
			t.Errorf("state panel missing %q", want)
		}
	}
	// The email row is the panel's evidence, never a second inbox item.
	if strings.Count(html, `<li class="flex items-start gap-3 py-3">`) != 1 {
		t.Errorf("inbox list rendered more than the in-app row")
	}
	if strings.Contains(html, "Email · sent") {
		t.Error("the panel counted another owner's deliveries (query is not owner-scoped)")
	}
}

// TestReadAndRemindPatchTheShellSurfaces pins the two surfaces the inbox writes
// outside #inbox: the rail's count bubble on both read actions, and the toast
// that confirms a scheduled reminder. Both are easy to drop in a refactor
// because the page still renders without them.
func TestReadAndRemindPatchTheShellSurfaces(t *testing.T) {
	h, q, _ := newFixture(t)
	h.SetRunStarter(stubRunStarter{})
	server, cookie := signedInServer(t, h, "surfaces@demo.app")
	u, err := q.GetUserByEmail(context.Background(), "surfaces@demo.app")
	if err != nil {
		t.Fatalf("get user: %v", err)
	}
	if err := h.Deliver(context.Background(), Delivery{
		Owner: u.ID, TransactionID: "surfaces:1", Workflow: "reminder",
		Subject: "Unread row", Body: "counts once",
	}); err != nil {
		t.Fatalf("deliver: %v", err)
	}

	readAll := postForm(t, server, cookie, "/api/notifications/read-all?clientID=tab-test", "")
	for _, want := range []string{
		"data: selector #inbox\n", "event: datastar-patch-signals",
		`data: signals {"inboxUnread":0}`,
	} {
		if !strings.Contains(readAll, want) {
			t.Errorf("read-all patch missing %q:\n%s", want, readAll)
		}
	}

	remind := postForm(t, server, cookie, "/api/notifications/remind", "text=Drink+water&delay=1m")
	for _, want := range []string{
		"data: selector #inbox\n", "data: selector #toast-container\n",
		"data: mode append\n", "Reminder scheduled.", "toastmsg_",
		`data: signals {"inboxUnread":0}`,
	} {
		if !strings.Contains(remind, want) {
			t.Errorf("remind patch missing %q:\n%s", want, remind)
		}
	}
	// The confirmation rides the toast frame, never the inbox frame: an inline
	// notice would keep the sentence on the page after the toast is gone.
	for frame := range strings.SplitSeq(remind, "event: datastar-patch-elements") {
		if strings.Contains(frame, "selector #inbox") && strings.Contains(frame, "Reminder scheduled.") {
			t.Errorf("the inbox patch still carries the scheduled notice:\n%s", frame)
		}
	}
}

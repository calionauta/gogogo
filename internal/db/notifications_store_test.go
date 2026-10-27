// SCOPE:core — generated-query contract for the notifications table.
package db

import (
	"context"
	"database/sql"
	"errors"
	"testing"

	dbgen "github.com/calionauta/gogogo/internal/db/sqlcgen"
	"github.com/calionauta/gogogo/internal/dbschema"
)

// createNotification writes one row with the shape a delivery step produces.
func createNotification(t *testing.T, q *dbgen.Queries, owner, txn, channel string) (dbgen.Notification, error) {
	t.Helper()
	return q.CreateNotification(context.Background(), dbgen.CreateNotificationParams{
		ID:            dbschema.NewID(),
		Owner:         owner,
		Workflow:      "todo-reminder",
		TransactionID: txn,
		Channel:       channel,
		Status:        "sent",
		Subject:       "Prazo hoje",
		Body:          "O item vence hoje.",
		Created:       dbschema.Now(),
	})
}

// TestNotificationQueriesAreOwnerScopedAndIdempotent pins the two properties
// the delivery layer relies on: a reader can never see another owner's rows,
// and a retried step cannot deliver the same transaction twice on one channel.
func TestNotificationQueriesAreOwnerScopedAndIdempotent(t *testing.T) {
	sqlDB := openTestDB(t)
	q := dbgen.New(sqlDB)
	ctx := context.Background()
	ada := createUser(t, q, "ada@demo.app")
	bob := createUser(t, q, "bob@demo.app")

	first, err := createNotification(t, q, ada.ID, "txn-1", "in_app")
	if err != nil {
		t.Fatalf("first CreateNotification: %v", err)
	}
	if first.Owner != ada.ID || first.Status != "sent" || first.ReadAt.Valid {
		t.Fatalf("created row = %+v, want owner=%s status=sent read=null", first, ada.ID)
	}

	// A duplicate delivery of the same transaction on the same channel is a
	// no-op, reported as ErrNoRows so the caller can tell "already delivered"
	// from "wrote now".
	if _, derr := createNotification(t, q, ada.ID, "txn-1", "in_app"); !errors.Is(derr, sql.ErrNoRows) {
		t.Fatalf("duplicate CreateNotification err = %v, want sql.ErrNoRows", derr)
	}

	// The same transaction on another channel is a distinct delivery.
	if _, cerr := createNotification(t, q, ada.ID, "txn-1", "email"); cerr != nil {
		t.Fatalf("second channel CreateNotification: %v", cerr)
	}

	// Bob must never see Ada's rows, even though the transaction id matches
	// nothing he owns. The list is the INBOX list: in_app only, so the email
	// copy is in the table without showing up twice on the page.
	mine, err := q.ListNotificationsForOwner(ctx, dbgen.ListNotificationsForOwnerParams{Owner: ada.ID, Limit: 10})
	if err != nil {
		t.Fatalf("ListNotificationsForOwner(ada): %v", err)
	}
	if len(mine) != 1 || mine[0].Channel != "in_app" {
		t.Fatalf("ada inbox rows = %+v, want exactly one in_app row", mine)
	}
	var delivered int
	countSQL := `SELECT count(*) FROM notifications WHERE owner = ?`
	if cerr := sqlDB.QueryRowContext(ctx, countSQL, ada.ID).Scan(&delivered); cerr != nil {
		t.Fatalf("count channels: %v", cerr)
	}
	if delivered != 2 {
		t.Fatalf("rows for ada = %d, want 2 (one per channel)", delivered)
	}
	his, err := q.ListNotificationsForOwner(ctx, dbgen.ListNotificationsForOwnerParams{Owner: bob.ID, Limit: 10})
	if err != nil {
		t.Fatalf("ListNotificationsForOwner(bob): %v", err)
	}
	if len(his) != 0 {
		t.Fatalf("bob rows = %d, want 0", len(his))
	}

	// Only in_app rows count as inbox unread.
	unread, err := q.CountUnreadInApp(ctx, ada.ID)
	if err != nil {
		t.Fatalf("CountUnreadInApp: %v", err)
	}
	if unread != 1 {
		t.Fatalf("unread = %d, want 1 (email rows are not inbox rows)", unread)
	}

	// A read transition is owner-scoped: Bob cannot mark Ada's row.
	if n, merr := q.MarkNotificationRead(ctx, dbgen.MarkNotificationReadParams{
		ReadAt: sql.NullString{String: dbschema.Now(), Valid: true},
		ID:     first.ID,
		Owner:  bob.ID,
	}); merr != nil || n != 0 {
		t.Fatalf("bob mark read = (%d, %v), want (0, nil)", n, merr)
	}
	if n, merr := q.MarkNotificationRead(ctx, dbgen.MarkNotificationReadParams{
		ReadAt: sql.NullString{String: dbschema.Now(), Valid: true},
		ID:     first.ID,
		Owner:  ada.ID,
	}); merr != nil || n != 1 {
		t.Fatalf("ada mark read = (%d, %v), want (1, nil)", n, merr)
	}
	// Marking an already-read row is a no-op, not a second write.
	if n, merr := q.MarkNotificationRead(ctx, dbgen.MarkNotificationReadParams{
		ReadAt: sql.NullString{String: dbschema.Now(), Valid: true},
		ID:     first.ID,
		Owner:  ada.ID,
	}); merr != nil || n != 0 {
		t.Fatalf("second mark read = (%d, %v), want (0, nil)", n, merr)
	}
	if unread, uerr := q.CountUnreadInApp(ctx, ada.ID); uerr != nil || unread != 0 {
		t.Fatalf("unread after read = (%d, %v), want (0, nil)", unread, uerr)
	}
}

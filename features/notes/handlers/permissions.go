// SCOPE:feature — Shared-doc permission rule (one home for author/admin).
//
// Notes and whiteboards are SHARED: every signed-in user opens every doc,
// which is what makes them prove live collaboration from two browsers.
// Two acts stay restricted, and they are deliberately NOT the same set:
//
//   - EDITING is open to every signed-in user, author or not. The demo
//     pair is the contract: demo2 (viewer) edits demo1's note and board.
//   - DELETING is the author's, plus an admin's. demo2 can never delete
//     what demo1 started; demo1 (admin) can delete anything.
//
// The rule is duplicated per feature on purpose (notes and whiteboard are
// separate bundles on this flat stack, and a shared helper would make one
// feature un-compilable without the other — check-flatdeps forbids a
// cross-feature domain import). Keeping the two copies textually
// identical is what keeps the contract honest; the tests in each package
// pin the same table.
//
// WRITE CONTRACT (registry check). Both write paths — the op batch and the
// stream rehydrate — refuse an id with no metadata row. Without it a session
// that invents an id got 200, persisted a snapshot into doc_snapshots, and
// left a row invisible to every list: an orphan costing storage and
// resurrectable by anyone who guessed the id again.
//
// The registry is a FIELD on the Handler (docRegistry), not a direct call to
// the queries, so a transport fixture can opt out explicitly instead of
// rewriting every synthetic id. Production wires the queries (NewHandler);
// only _test.go files replace it, and they say so at the replacement site.
// There is no env flag and no test-name sniffing: a bypass that a request
// cannot reach is the only kind worth having.
package handlers

import (
	"context"

	"github.com/calionauta/gogogo/internal/rbac"
)

// canDeleteDoc reports whether the requester may delete a doc it can
// already see. Author first (every role — a viewer deletes what they
// started), then admin. Everyone else is refused, including a plain
// "user" peer: a destructive act on someone else's work is never implied
// by being able to edit it.
func canDeleteDoc(role, requester, author string) bool {
	if requester != "" && requester == author {
		return true
	}
	return role == rbac.RoleAdmin
}

// docRegistry reports whether a doc id has a metadata row. It is the seam
// the write paths ask instead of calling the queries directly, so a
// transport fixture can drive synthetic ids without disabling the check in
// production code (see the header).
type docRegistry func(ctx context.Context, docID string) bool

// registeredDoc is the production registry: the notes table is the index,
// so a row is exactly what "the doc exists" means.
func (h *Handler) registeredDoc(ctx context.Context, docID string) bool {
	if h.registry != nil {
		return h.registry(ctx, docID)
	}
	_, err := h.q.GetNoteMeta(ctx, docID)
	return err == nil
}

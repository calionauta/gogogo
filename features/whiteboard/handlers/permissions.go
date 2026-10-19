// SCOPE:feature — Shared-doc permission rule (one home for author/admin).
//
// Identical to the notes slice's rule, and deliberately duplicated rather
// than shared: notes and whiteboard are separate bundles on this flat
// stack, and a shared helper package would make one feature un-compilable
// without the other (check-flatdeps forbids a cross-feature domain
// import). Both copies are pinned by the same test table.
//
// Boards are SHARED — every signed-in user opens every board, which is
// what makes them prove live collaboration from two browsers. Editing is
// open to every signed-in user; DELETING is the author's, plus an admin's.
// demo2 (viewer) edits demo1's board and can never delete it; demo1
// (admin) deletes anything.
//
// WRITE CONTRACT (registry check). The board write path refuses an id with
// no metadata row, so a session that invents an id cannot persist shapes no
// list will ever show (the orphan that costs storage and reappears for
// anyone who guesses the id again).
//
// The registry is a FIELD on the Handler (registry), not a direct queries
// call, so a transport fixture opts out explicitly instead of rewriting
// every synthetic id. Production wires the queries (NewHandler); the full
// rationale lives in notes/handlers/permissions.go.
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

// docRegistry reports whether a doc id has a metadata row (see the header).
type docRegistry func(ctx context.Context, docID string) bool

// registeredDoc is the production registry: the boards table is the index.
func (h *Handler) registeredDoc(ctx context.Context, docID string) bool {
	if h.registry != nil {
		return h.registry(ctx, docID)
	}
	_, err := h.q.GetBoardMeta(ctx, docID)
	return err == nil
}

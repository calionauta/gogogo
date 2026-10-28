// SCOPE:feature — exactly-once effect for a mutation delivered more than once.
package handlers

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"errors"
	"net/http"
	"strings"

	dbgen "github.com/calionauta/gogogo/internal/db/sqlcgen"
	"github.com/calionauta/gogogo/internal/dbschema"
)

// maxOpKeyLen bounds the client-supplied token. A UUID is 36 bytes; the limit
// keeps a hostile client from writing megabytes into the primary key while
// leaving room for a future prefixed form ("tab:op-id").
const maxOpKeyLen = 128

// errOpAlreadyApplied unwinds the receipt transaction without touching the
// row. It is a control signal, never surfaced to the client.
var errOpAlreadyApplied = errors.New("todo: operation already applied")

// idempotencyKey reads the outbox's token for this intent. Empty means the
// caller opted out: the mutation keeps its plain at-least-once semantics, so
// curl, a script or a future non-outbox client is unaffected.
//
// An over-long key is HASHED, not truncated. Truncation makes two distinct
// intents share a receipt — the second one silently disappears — and dropping
// the key would let a replay apply twice. The hash keeps both properties: a
// distinct key per intent, bounded storage.
func idempotencyKey(r *http.Request) string {
	key := strings.TrimSpace(r.Header.Get("Idempotency-Key"))
	if key == "" {
		return ""
	}
	if len(key) > maxOpKeyLen {
		sum := sha256.Sum256([]byte(key))
		return "sha256:" + hex.EncodeToString(sum[:])
	}
	return key
}

// applyOnce runs work at most once per (owner, key).
//
// The claim and the work share ONE transaction: a commit records both, a
// rollback records neither. That is the property a separate "mark as done"
// statement cannot give — a crash between the two would either lose a write
// (receipt without effect) or duplicate one (effect without receipt).
//
// Returns applied=false when this delivery is a replay: the caller must answer
// without re-running the effect and without emitting a second UI patch.
//
// A keyless request runs work directly (no transaction): a single statement is
// already atomic, and wrapping it would be a pass-through layer.
func (h *Handler) applyOnce(
	r *http.Request, owner string, work func(context.Context, *dbgen.Queries) error,
) (bool, error) {
	key := idempotencyKey(r)
	if key == "" {
		return true, work(r.Context(), h.Q)
	}
	if h.DB == nil {
		// No database handle means no transaction (a test fixture that only
		// wired the queries): run the work rather than silently dropping the
		// request. Production always has DB set.
		return true, work(r.Context(), h.Q)
	}
	err := dbschema.Transact(r.Context(), h.DB, "todo op receipt", func(ctx context.Context, tx *sql.Tx) error {
		q := h.Q.WithTx(tx)
		claimed, cerr := q.ClaimOpReceipt(ctx, dbgen.ClaimOpReceiptParams{
			Owner: owner, OpKey: key, Created: dbschema.Now(),
		})
		if cerr != nil {
			return cerr
		}
		if claimed == 0 {
			return errOpAlreadyApplied
		}
		return work(ctx, q)
	})
	if errors.Is(err, errOpAlreadyApplied) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	return true, nil
}

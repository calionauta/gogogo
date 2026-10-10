// SCOPE:layer=feature,removal=feature — chat demo store (in-memory)
package chat

import (
	"context"
	"sync"
	"time"
)

// MemStore is the demo MessageStore: append-only, owner-scoped,
// idem-deduped, created-ordered, zero I/O. Production history wants a
// PocketBase collection (owner + idem_key unique index, same shape as
// the todo idempotency hook) — wire it behind MessageStore when the
// demo grows history UI; the worker contract stays identical.
type MemStore struct {
	mu   sync.Mutex
	msgs []Message
	seen map[string]bool
}

// NewMemStore constructs an empty demo store.
func NewMemStore() *MemStore { return &MemStore{seen: map[string]bool{}} }

// Append stores one record; a repeated (owner, idemKey) is a no-op.
// Empty idemKey always stores (no dedup key to match on).
func (s *MemStore) Append(_ context.Context, owner string, m Message) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if m.IdemKey != "" {
		k := owner + "\x00" + m.IdemKey
		if s.seen[k] {
			return nil
		}
		s.seen[k] = true
	}
	m.Owner = owner
	m.Created = time.Now()
	s.msgs = append(s.msgs, m)
	return nil
}

// List returns one owner's records in created order. Never nil —
// renderers range over the result without a nil check.
func (s *MemStore) List(_ context.Context, owner string) ([]Message, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := []Message{}
	for _, m := range s.msgs {
		if m.Owner == owner {
			out = append(out, m)
		}
	}
	return out, nil
}

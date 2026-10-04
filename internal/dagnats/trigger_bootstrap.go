// SCOPE:layer=infra,removal=plugin — WORKAROUND for an upstream DagNats bug.
//
// # What this is
//
// A self-contained shim that seeds the DagNats trigger KV bucket with one
// disabled placeholder trigger on first boot. It exists ONLY because
// DagNats v0.0.24 cannot create the first trigger through its own console.
//
// # The upstream bug
//
// internal/api/service_triggers.go in DagNats v0.0.24:
//
//	keys, err := s.triggerKV.Keys(ctx)
//	if err != nil {
//		return nil, err
//	}
//
// When the "triggers" bucket exists but holds no keys, Keys() returns
// jetstream.ErrNoKeysFound and that error is returned RAW. Every other KV
// read in that package treats the same condition as "nothing here" (25 call
// sites do `errors.Is(err, jetstream.ErrNoKeysFound)`, including
// checkHTTPRouteConflict inside CreateTrigger itself), so this one is an
// inconsistency in the upstream code, not a design decision.
//
// The console calls ListTriggers() BEFORE writing, to check for a duplicate
// id. So on a fresh install:
//
//	POST /console/triggers -> ListTriggers -> ErrNoKeysFound
//	  -> "lookup failed: nats: no keys found" with HTTP 500, nothing written
//
// The bucket therefore never becomes non-empty, and the console — the only
// surface that can create a trigger — stays broken forever. Verified by
// driving the real server: two consecutive POSTs both return 500 and the
// bucket stays empty.
//
// # Why seeding works, and why the seed is a real trigger
//
// Seeding any key makes ListTriggers() succeed. The seed must be a REAL,
// DISABLED TriggerDef rather than a marker key, because the console renders
// every KV entry as a trigger row: a `_marker` key shows up in the UI as a
// phantom trigger with kind "unknown". A disabled definition renders as an
// ordinary trigger, fires nothing (Enabled: false), and the operator can
// delete it once they have created their own. Verified both ways.
//
// # Removing this
//
// Delete this file, drop the DagNats.TriggerBootstrap config field, and
// remove the call in cmd/web/dagnats.go. No other code depends on it — the
// seed is ordinary KV data. Re-evaluate on every DagNats upgrade: if
// listTriggersInner stops returning the raw error, this shim is dead weight.
// See docs/dagnats-bootstrap-workaround.md for the re-evaluation checklist.
package dagnats

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"time"

	"github.com/nats-io/nats.go"
	"github.com/nats-io/nats.go/jetstream"
)

const (
	// triggerBucket is the KV bucket DagNats stores trigger definitions in.
	// Pinned here deliberately: if upstream renames it, EnsureTriggerBucket
	// fails loudly at boot instead of silently seeding nothing.
	triggerBucket = "triggers"

	// bootstrapTriggerID is the id of the seeded placeholder. Underscore
	// prefix keeps it visibly non-user in the console list.
	bootstrapTriggerID = "_bootstrap"

	// bootstrapTriggerKind is the trigger kind the placeholder uses. Cron
	// is the cheapest definition and needs no external endpoint.
	bootstrapTriggerCron = "0 3 * * *"

	// bootstrapSource labels the seed so a future reader can tell it apart
	// from console- or CLI-created triggers. TriggerDef.Source is upstream's
	// own field for exactly this.
	bootstrapSource = "bootstrap"

	// bootstrapTimeout bounds the whole ensure operation. It runs on the
	// boot path, so it must not be able to hang startup.
	bootstrapTimeout = 15 * time.Second
)

// placeholderTrigger mirrors the subset of DagNats' trigger.TriggerDef that
// the seed needs. Defined locally instead of importing
// github.com/danmestas/dagnats/internal/trigger because that package is
// internal to the upstream module and cannot be imported here.
//
// The JSON tags are snake_case because they must match the upstream wire
// format exactly — the value is read back by upstream's own unmarshaler, so
// tagliatelle's camelCase preference does not apply here.
//
//nolint:tagliatelle // wire format is fixed by the upstream contract
type placeholderTrigger struct {
	ID         string           `json:"id"`
	WorkflowID string           `json:"workflow_id"`
	Enabled    bool             `json:"enabled"`
	Cron       *placeholderCron `json:"cron,omitempty"`
	Source     string           `json:"source,omitempty"`
}

type placeholderCron struct {
	Expression string `json:"expression"`
}

// EnsureTriggerBucket makes the DagNats trigger bucket safe to use by
// guaranteeing it is never empty. Safe to call on every boot: it reads
// first and writes only when the bucket has no keys, so an existing
// installation is never touched.
//
// workflowID should be a workflow that exists in the engine, so the
// placeholder is a valid definition rather than a dangling reference.
//
// Returns nil when the bucket is already usable. Errors are returned for
// the caller to log — this runs on the boot path and must never fatal, so
// callers should warn and continue.
func EnsureTriggerBucket(nc *nats.Conn, workflowID string) error {
	if nc == nil {
		return errors.New("dagnats bootstrap: nil NATS connection")
	}
	if workflowID == "" {
		return errors.New("dagnats bootstrap: empty workflow id")
	}

	ctx, cancel := context.WithTimeout(context.Background(), bootstrapTimeout)
	defer cancel()

	js, err := jetstream.New(nc)
	if err != nil {
		return fmt.Errorf("dagnats bootstrap: jetstream: %w", err)
	}

	kv, err := js.KeyValue(ctx, triggerBucket)
	if err != nil {
		// The bucket is created by the engine at startup. Missing it means
		// the engine is not up yet or the name changed — both worth
		// surfacing rather than guessing.
		return fmt.Errorf("dagnats bootstrap: open %s bucket: %w", triggerBucket, err)
	}

	keys, err := kv.Keys(ctx)
	switch {
	case err == nil && len(keys) > 0:
		// Normal case for every boot after the first: leave it alone.
		slog.Debug("dagnats bootstrap: trigger bucket already populated, no seed needed",
			"keys", len(keys))
		return nil
	case err != nil && !errors.Is(err, jetstream.ErrNoKeysFound):
		return fmt.Errorf("dagnats bootstrap: list %s keys: %w", triggerBucket, err)
	}

	// Empty bucket — the exact condition that breaks the console.
	seed := placeholderTrigger{
		ID:         bootstrapTriggerID,
		WorkflowID: workflowID,
		Enabled:    false, // never fires
		Cron:       &placeholderCron{Expression: bootstrapTriggerCron},
		Source:     bootstrapSource,
	}
	payload, err := json.Marshal(seed)
	if err != nil {
		return fmt.Errorf("dagnats bootstrap: marshal seed: %w", err)
	}
	if _, err := kv.Put(ctx, bootstrapTriggerID, payload); err != nil {
		return fmt.Errorf("dagnats bootstrap: put seed: %w", err)
	}

	slog.Warn("dagnats bootstrap: seeded a placeholder trigger — WORKAROUND for "+
		"DagNats v0.0.24 (see docs/dagnats-bootstrap-workaround.md). "+
		"The trigger console cannot create the first trigger without it. "+
		"Safe to delete this placeholder once you have created your own.",
		"id", bootstrapTriggerID, "workflow", workflowID, "enabled", false)
	return nil
}

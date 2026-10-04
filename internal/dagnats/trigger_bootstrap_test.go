// SCOPE:layer=infra,removal=plugin — tests for the DagNats trigger-bootstrap
// workaround. Delete alongside trigger_bootstrap.go.
package dagnats

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"

	natsserver "github.com/nats-io/nats-server/v2/server"
	"github.com/nats-io/nats.go"
	"github.com/nats-io/nats.go/jetstream"
)

// bootNATSWithBucket starts an in-process NATS with JetStream and creates the
// "triggers" bucket empty — the exact precondition that breaks the console.
func bootNATSWithBucket(t *testing.T) *nats.Conn {
	t.Helper()
	opts := &natsserver.Options{
		Host:      "127.0.0.1",
		Port:      -1, // random free port
		JetStream: true,
		StoreDir:  t.TempDir(),
		NoLog:     true,
		NoSigs:    true,
	}
	ns, err := natsserver.NewServer(opts)
	if err != nil {
		t.Fatalf("nats server: %v", err)
	}
	go ns.Start()
	if !ns.ReadyForConnections(10 * time.Second) {
		t.Fatal("nats server not ready")
	}
	t.Cleanup(ns.Shutdown)

	nc, err := nats.Connect(ns.ClientURL())
	if err != nil {
		t.Fatalf("nats connect: %v", err)
	}
	t.Cleanup(nc.Close)

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	js, err := jetstream.New(nc)
	if err != nil {
		t.Fatalf("jetstream.New: %v", err)
	}
	if _, err := js.CreateKeyValue(ctx, jetstream.KeyValueConfig{Bucket: triggerBucket}); err != nil {
		t.Fatalf("create %s bucket: %v", triggerBucket, err)
	}
	return nc
}

func readTriggers(t *testing.T, nc *nats.Conn) []string {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	js, err := jetstream.New(nc)
	if err != nil {
		t.Fatalf("jetstream.New: %v", err)
	}
	kv, err := js.KeyValue(ctx, triggerBucket)
	if err != nil {
		t.Fatalf("open bucket: %v", err)
	}
	keys, err := kv.Keys(ctx)
	if err != nil {
		// ErrNoKeysFound is the empty case, which is the thing under test.
		return nil
	}
	return keys
}

// TestEnsureTriggerBucket_SeedsEmptyBucket is the core behaviour: an empty
// bucket gets exactly one seed, and the seed is a real disabled definition
// rather than a marker (a marker renders as a phantom row in the console).
func TestEnsureTriggerBucket_SeedsEmptyBucket(t *testing.T) {
	nc := bootNATSWithBucket(t)

	if keys := readTriggers(t, nc); len(keys) != 0 {
		t.Fatalf("precondition: expected empty bucket, got %v", keys)
	}

	if err := EnsureTriggerBucket(nc, "onboarding"); err != nil {
		t.Fatalf("EnsureTriggerBucket: %v", err)
	}

	keys := readTriggers(t, nc)
	if len(keys) != 1 {
		t.Fatalf("expected exactly 1 seeded key, got %d: %v", len(keys), keys)
	}
	if keys[0] != bootstrapTriggerID {
		t.Fatalf("seeded key = %q, want %q", keys[0], bootstrapTriggerID)
	}

	// The seed must be a valid, DISABLED TriggerDef with a real workflow.
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	js, _ := jetstream.New(nc)
	kv, _ := js.KeyValue(ctx, triggerBucket)
	entry, err := kv.Get(ctx, bootstrapTriggerID)
	if err != nil {
		t.Fatalf("get seed: %v", err)
	}

	var got placeholderTrigger
	if err := json.Unmarshal(entry.Value(), &got); err != nil {
		t.Fatalf("seed is not valid JSON for a TriggerDef: %v", err)
	}
	if got.Enabled {
		t.Errorf("seed must be DISABLED so it never fires, got enabled=true")
	}
	if got.WorkflowID == "" {
		t.Errorf("seed must reference a real workflow, got empty workflow_id")
	}
	if got.Cron == nil || got.Cron.Expression == "" {
		t.Errorf("seed must carry a cron expression, got %+v", got.Cron)
	}
	if got.Source != bootstrapSource {
		t.Errorf("seed source = %q, want %q", got.Source, bootstrapSource)
	}
}

// TestEnsureTriggerBucket_Idempotent guards the property that matters for a
// boot-path call: running it on a populated bucket must not add or change
// anything. Without this, every restart would inject another placeholder.
func TestEnsureTriggerBucket_Idempotent(t *testing.T) {
	nc := bootNATSWithBucket(t)

	if err := EnsureTriggerBucket(nc, "onboarding"); err != nil {
		t.Fatalf("first call: %v", err)
	}
	first := readTriggers(t, nc)

	// Simulate a restart: the same call on a now-populated bucket.
	for range 3 {
		if err := EnsureTriggerBucket(nc, "onboarding"); err != nil {
			t.Fatalf("repeat call: %v", err)
		}
	}
	after := readTriggers(t, nc)

	if len(after) != len(first) {
		t.Fatalf("repeated calls changed the bucket: %v -> %v", first, after)
	}
	if len(after) != 1 {
		t.Fatalf("expected the single seed to remain, got %v", after)
	}
}

// TestEnsureTriggerBucket_LeavesExistingTriggersAlone proves an installation
// that already has its own triggers is never touched — the shim must not
// displace real data.
func TestEnsureTriggerBucket_LeavesExistingTriggersAlone(t *testing.T) {
	nc := bootNATSWithBucket(t)

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	js, _ := jetstream.New(nc)
	kv, _ := js.KeyValue(ctx, triggerBucket)

	// A pre-existing, user-created trigger.
	userTrigger := `{"id":"my-cron","workflow_id":"onboarding","enabled":true,` +
		`"cron":{"expression":"*/5 * * * *"},"source":"console"}`
	if _, err := kv.Put(ctx, "my-cron", []byte(userTrigger)); err != nil {
		t.Fatalf("seed user trigger: %v", err)
	}

	if err := EnsureTriggerBucket(nc, "onboarding"); err != nil {
		t.Fatalf("EnsureTriggerBucket: %v", err)
	}

	keys := readTriggers(t, nc)
	if len(keys) != 1 || keys[0] != "my-cron" {
		t.Fatalf("existing triggers must be untouched; got %v", keys)
	}

	entry, err := kv.Get(ctx, "my-cron")
	if err != nil {
		t.Fatalf("get user trigger: %v", err)
	}
	if !strings.Contains(string(entry.Value()), `"enabled":true`) {
		t.Errorf("the user trigger was modified: %s", entry.Value())
	}
}

// TestEnsureTriggerBucket_RejectsBadInput keeps the boot path safe: this is
// called with values derived from config, so an empty workflow id must be a
// returned error rather than a seed pointing nowhere.
func TestEnsureTriggerBucket_RejectsBadInput(t *testing.T) {
	nc := bootNATSWithBucket(t)

	if err := EnsureTriggerBucket(nil, "onboarding"); err == nil {
		t.Error("nil connection must be an error")
	}
	if err := EnsureTriggerBucket(nc, ""); err == nil {
		t.Error("empty workflow id must be an error")
	}
	if keys := readTriggers(t, nc); len(keys) != 0 {
		t.Errorf("a rejected call must not seed anything; got %v", keys)
	}
}

// TestEnsureTriggerBucket_MissingBucketErrors makes the failure mode loud: if
// upstream renames the bucket this must surface, not silently seed nothing.
func TestEnsureTriggerBucket_MissingBucketErrors(t *testing.T) {
	opts := &natsserver.Options{
		Host: "127.0.0.1", Port: -1, JetStream: true,
		StoreDir: t.TempDir(), NoLog: true, NoSigs: true,
	}
	ns, err := natsserver.NewServer(opts)
	if err != nil {
		t.Fatalf("nats server: %v", err)
	}
	go ns.Start()
	if !ns.ReadyForConnections(10 * time.Second) {
		t.Fatal("nats not ready")
	}
	t.Cleanup(ns.Shutdown)
	nc, err := nats.Connect(ns.ClientURL())
	if err != nil {
		t.Fatalf("connect: %v", err)
	}
	t.Cleanup(nc.Close)

	// Note: no "triggers" bucket created.
	if err := EnsureTriggerBucket(nc, "onboarding"); err == nil {
		t.Fatal("expected an error when the triggers bucket does not exist")
	}
}

// SCOPE:plugin — NATS JetStream + Leaf Node + CRUD proxy
package nats

import (
	"errors"
	"fmt"
	"time"

	"github.com/nats-io/nats.go"
)

// EnsureStream creates a stream if it doesn't exist on the CURRENT handle's
// JetStream. It returns an error when no handle has been started.
func EnsureStream(name string, subjects []string, maxAge ...time.Duration) error {
	js := JetStream()
	if js == nil {
		return errors.New("nats: no JetStream — StartEmbedded/ConnectExisting was not called")
	}
	cfg := &nats.StreamConfig{
		Name:      name,
		Subjects:  subjects,
		Storage:   nats.FileStorage,
		Retention: nats.LimitsPolicy,
	}
	if len(maxAge) > 0 {
		cfg.MaxAge = maxAge[0]
	}
	_, err := js.AddStream(cfg)
	if err == nil {
		return nil
	}
	// The old comment here said "stream already exists is not an error" and
	// then returned the error anyway — so the ONE case it claimed to tolerate
	// was the one that failed. The comment is gone and the behaviour is now
	// what the comment promised.
	return reconcileExisting(js, cfg, err)
}

// EnsureStreamOn adds a stream on an EXPLICIT JetStream context, reconciling
// one that already exists.
//
// The explicit-context twin of EnsureStream, which reads the current handle:
// the realtime broadcaster and the CRUD publisher each hold their own context
// (an embedded server's, or one shared with a DagNats engine), so they cannot
// route through a process global.
//
// The toleration is not cosmetic, and the string comparison it replaces was
// wrong twice over:
//
//  1. `err.Error() != "stream already exists"` never matched anything. The
//     server answers "stream name already in use", surfaced as the sentinel
//     ErrStreamNameAlreadyInUse — so both copies of that check were dead code
//     that only LOOKED like handling.
//  2. The error only happens when the config CHANGED. A second AddStream with
//     an identical config returns nil (the server treats it as a no-op), so
//     what this must absorb is precisely the UPGRADE case: a release that edits
//     MaxAge or MaxBytes. Treated as fatal, it silently disabled the CRUD
//     publisher (nil publisher, and the boot's nil-safe path means
//     cross-instance sync quietly stops) and failed the realtime broadcaster
//     outright.
//
// UpdateStream reconciles the existing stream onto the requested config, so a
// changed bound takes effect instead of being ignored or fatal.
func EnsureStreamOn(js nats.JetStreamContext, cfg *nats.StreamConfig) error {
	if _, err := js.AddStream(cfg); err != nil {
		return reconcileExisting(js, cfg, err)
	}
	return nil
}

// reconcileExisting turns an "already in use" error into an UpdateStream, and
// passes every other error through untouched.
func reconcileExisting(js nats.JetStreamContext, cfg *nats.StreamConfig, addErr error) error {
	if !errors.Is(addErr, nats.ErrStreamNameAlreadyInUse) {
		return addErr
	}
	if _, uerr := js.UpdateStream(cfg); uerr != nil {
		return fmt.Errorf("nats: update stream %s: %w", cfg.Name, uerr)
	}
	return nil
}

// EnsureKeyValue creates a KV bucket if it doesn't exist on the CURRENT
// handle's JetStream. history caps per-key revisions (1 = latest only,
// right for coordination state like rosters); ttl expires keys no one
// refreshes (crashed members vanish instead of accumulating forever).
// Zero history keeps the server default; zero ttl means no expiry.
func EnsureKeyValue(bucket string, history uint8, ttl time.Duration, maxValueSize ...int32) (nats.KeyValue, error) {
	js := JetStream()
	if js == nil {
		return nil, errors.New("nats: no JetStream — StartEmbedded/ConnectExisting was not called")
	}
	cfg := &nats.KeyValueConfig{
		Bucket:  bucket,
		Storage: nats.FileStorage,
		History: history,
		TTL:     ttl,
	}
	if len(maxValueSize) > 0 {
		cfg.MaxValueSize = maxValueSize[0]
	}
	kv, err := js.CreateKeyValue(cfg)
	if err != nil {
		return nil, err
	}
	return kv, nil
}

// PublishEvent publishes an event to a room stream on the CURRENT handle.
func PublishEvent(roomID, eventType string, data []byte) error {
	js := JetStream()
	if js == nil {
		return errors.New("nats: no JetStream — StartEmbedded/ConnectExisting was not called")
	}
	_, err := js.Publish("room."+roomID+"."+eventType, data)
	return err
}

// SubscribeRoom subscribes to all events for a room on the CURRENT handle.
func SubscribeRoom(roomID string, handler func(msg *nats.Msg)) (*nats.Subscription, error) {
	js := JetStream()
	if js == nil {
		return nil, errors.New("nats: no JetStream — StartEmbedded/ConnectExisting was not called")
	}
	return js.Subscribe("room."+roomID+".>", handler)
}

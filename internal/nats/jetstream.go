// SCOPE:layer=infra,removal=plugin — NATS JetStream + Leaf Node + CRUD proxy
package nats

import (
	"errors"
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
	// Stream already exists is not an error
	return err
}

// EnsureKeyValue creates a KV bucket if it doesn't exist on the CURRENT
// handle's JetStream.
func EnsureKeyValue(bucket string, maxValueSize ...int32) (nats.KeyValue, error) {
	js := JetStream()
	if js == nil {
		return nil, errors.New("nats: no JetStream — StartEmbedded/ConnectExisting was not called")
	}
	cfg := &nats.KeyValueConfig{
		Bucket:  bucket,
		Storage: nats.FileStorage,
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

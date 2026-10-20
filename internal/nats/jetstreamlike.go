// SCOPE:plugin — JetStream handle shape shared by the wiring
package nats

import natsio "github.com/nats-io/nats.go"

// JetStreamLike is the shape StartEmbedded / ConnectExisting return, so
// callers (server boot, cmd/web) do not import nats.go directly just to
// name the type.
type JetStreamLike = natsio.JetStreamContext

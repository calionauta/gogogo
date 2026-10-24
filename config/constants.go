// SCOPE:core — Process-wide runtime constants (not env knobs).
package config

import "time"

// DefaultReplayBufferSize is the per-client SSEHub ring-buffer length.
// Sized for ~64KB of Datastar patch-signals at ~1KB each.
const DefaultReplayBufferSize = 64

// DefaultClientQueueSize is the per-client SSEHub channel buffer.
// Both the todo and whiteboard SSE handlers use 64 slots — tune
// this constant globally.
const DefaultClientQueueSize = 64

// DefaultSSEHeartbeatInterval is how often the SSE handler writes a
// comment line (: heartbeat) to detect client disconnection. 15s is
// the industry standard for SSE heartbeats.
const DefaultSSEHeartbeatInterval = 15 * time.Second

// defaultRateLimitRPM is the per-session budget when RATE_LIMIT_RPM is unset.
// 20 req/s is far above a human page's fan-out and far below a runaway loop.
const defaultRateLimitRPM = 1200

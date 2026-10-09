// SCOPE:layer=infra,removal=plugin — Loro CRDT + DocStore + sync workers + presence
package collab

import (
	"encoding/json"
	"fmt"
)

// PresenceMsg is one ephemeral presence event shared by both the
// jetstream and web-only transports. Type is "cursor" | "join" | "leave"
// | "count". Cursor carries normalized coordinates (0..1) so any viewport
// can place a remote cursor regardless of the peer's canvas size.
//
// Declared here (build-tag-free) so presence_web.go and presence.go
// (jetstream) share a single definition.
//
// UnmarshalJSON is tolerant of coordinate values sent as JSON strings
// (e.g. x:"0.5") OR numbers (x:0.5). This keeps the endpoint robust even
// if a client (or a fork of this template) posts stringified numbers — the
// server never 400s a perfectly good cursor event on a type-coercion nit.
type PresenceMsg struct {
	Type string  `json:"type"`
	Doc  string  `json:"doc"`
	User string  `json:"user"`
	X    float64 `json:"x"`
	Y    float64 `json:"y"`
	TS   int64   `json:"ts"`
	// CTS is the SENDER-captured timestamp (Unix millis at frame build).
	// The server re-stamps TS on receipt, which destroys capture order —
	// so receivers adjudicate staleness with CTS, compared only within one
	// sender's frames (per-user key), where the sender clock is monotonic
	// and cross-machine skew cannot intrude. A retried POST keeps its
	// original CTS and always loses to anything captured later. Passes
	// through the server untouched (only TS is re-stamped).
	CTS int64 `json:"cts,omitempty"`
	// Peers is the list of clientIDs already present on the doc, sent to
	// a freshly-connected client as a "snapshot" event so it can seed its
	// peer count without waiting for future joins (which already happened
	// before it connected). For the authoritative "count" event it carries
	// the FULL set of currently-connected clientIDs (including the recipient),
	// and the client excludes itself when rendering.
	Peers []string `json:"peers,omitempty"`

	// Shape carries an IN-PROGRESS ("draft") shape for an ephemeral
	// `type:"draft"` event — the whiteboard's live-drawing channel. It is
	// deliberately NOT part of the CRDT: a half-drawn shape is not committed
	// state, so it is relayed over the same volatile presence path as a
	// cursor (never persisted, never merged, never sent to NATS) and is
	// superseded by the real `add` op on pointer-up. This is what lets peers
	// watch a stroke grow instead of seeing it pop in whole on release.
	Shape *Shape `json:"shape,omitempty"`
}

// presenceAlias mirrors PresenceMsg but decodes X/Y as json.Number so a
// numeric string like "0.5" and a bare number 0.5 both parse.
type presenceAlias struct {
	Type  string      `json:"type"`
	Doc   string      `json:"doc"`
	User  string      `json:"user"`
	X     json.Number `json:"x"`
	Y     json.Number `json:"y"`
	TS    int64       `json:"ts"`
	CTS   int64       `json:"cts,omitempty"`
	Peers []string    `json:"peers,omitempty"`
	Shape *Shape      `json:"shape,omitempty"`
}

// UnmarshalJSON tolerates X/Y provided as JSON numbers or numeric strings.
func (m *PresenceMsg) UnmarshalJSON(b []byte) error {
	var a presenceAlias
	if err := json.Unmarshal(b, &a); err != nil {
		return err
	}
	m.Type = a.Type
	m.Doc = a.Doc
	m.User = a.User
	m.TS = a.TS
	m.CTS = a.CTS
	m.Peers = a.Peers
	m.Shape = a.Shape
	if a.X != "" {
		f, err := a.X.Float64()
		if err != nil {
			return fmt.Errorf("presence x: %w", err)
		}
		m.X = f
	}
	if a.Y != "" {
		f, err := a.Y.Float64()
		if err != nil {
			return fmt.Errorf("presence y: %w", err)
		}
		m.Y = f
	}
	return nil
}

// DisplayName resolves the human identity for presence: the authed email
// when signed in, else the raw client id. Whiteboard and notes share it
// so two demo accounts read apart everywhere (and a spoofed client-side
// user never reaches peers — the server stamps what it authenticated).
func DisplayName(email, fallback string) string {
	if email != "" {
		return email
	}
	return fallback
}

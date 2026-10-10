// SCOPE:layer=infra,removal=plugin — AG-UI-compatible event envelope for AI streams
// Package genui speaks the AG-UI wire format (ag-ui-protocol/ag-ui) for the
// event subset gogogo emits, without the external SDK: the community Go SDK
// covers encode/decode/client, but vendoring it buys version churn for ~10
// string constants — the envelope below is byte-compatible by test (see
// TestDecodeReadsReferenceSDKOutput) and travels the existing SSE Hub, so
// there is no new transport and no new dependency.
//
// Scope is DELIBERATELY the framing subset (run open/close + text message).
// Tool-call and state events arrive with the component catalog, one const
// plus one validation line each — not before a renderer consumes them
// (YAGNI: untested API surface is debt, not coverage).
package genui

import (
	"encoding/json"
	"errors"
	"fmt"

	"github.com/calionauta/gogogo/internal/queue"
)

// EventType mirrors AG-UI protocol wire names. Only registered types
// encode; anything else fails closed at Encode and Decode.
type EventType string

// Registered event types (framing subset).
const (
	EventTypeTextMessageStart   EventType = "TEXT_MESSAGE_START"
	EventTypeTextMessageContent EventType = "TEXT_MESSAGE_CONTENT"
	EventTypeTextMessageEnd     EventType = "TEXT_MESSAGE_END"
	EventTypeRunStarted         EventType = "RUN_STARTED"
	EventTypeRunFinished        EventType = "RUN_FINISHED"
	EventTypeRunError           EventType = "RUN_ERROR"
)

// registered is the emit allow-list: fail-closed by construction.
var registered = map[EventType]bool{
	EventTypeTextMessageStart:   true,
	EventTypeTextMessageContent: true,
	EventTypeTextMessageEnd:     true,
	EventTypeRunStarted:         true,
	EventTypeRunFinished:        true,
	EventTypeRunError:           true,
}

// Event is one AG-UI wire message. Field names match the protocol's
// camelCase JSON contract; empty fields are omitted on the wire.
type Event struct {
	Type      EventType `json:"type"`
	MessageID string    `json:"messageId,omitempty"`
	Delta     string    `json:"delta,omitempty"`
	RunID     string    `json:"runId,omitempty"`
	ThreadID  string    `json:"threadId,omitempty"`
	Error     string    `json:"error,omitempty"`
}

// validate enforces per-type required fields. Message scoping (a content
// for a never-started message) is the stream driver's job, not the
// envelope's — same split as the reference SDK's Validate vs
// ValidateSequence.
func (e Event) validate() error {
	if !registered[e.Type] {
		return fmt.Errorf("genui: unregistered event type %q", e.Type)
	}
	switch e.Type {
	case EventTypeTextMessageStart, EventTypeTextMessageContent, EventTypeTextMessageEnd:
		if e.MessageID == "" {
			return fmt.Errorf("genui: %s requires messageId", e.Type)
		}
		if e.Type == EventTypeTextMessageContent && e.Delta == "" {
			return errors.New("genui: TEXT_MESSAGE_CONTENT requires delta")
		}
	case EventTypeRunStarted, EventTypeRunFinished, EventTypeRunError:
		if e.RunID == "" {
			return fmt.Errorf("genui: %s requires runId", e.Type)
		}
	}
	return nil
}

// Encode validates and marshals one event to AG-UI wire bytes.
func Encode(e Event) ([]byte, error) {
	if err := e.validate(); err != nil {
		return nil, err
	}
	raw, err := json.Marshal(e)
	if err != nil {
		return nil, fmt.Errorf("genui: marshal: %w", err)
	}
	return raw, nil
}

// Decode parses AG-UI wire bytes (ours or a reference SDK's — unknown
// fields like timestamp are ignored) and rejects unknown types.
func Decode(raw []byte) (Event, error) {
	var probe struct {
		Type EventType `json:"type"`
	}
	if err := json.Unmarshal(raw, &probe); err != nil {
		return Event{}, fmt.Errorf("genui: decode envelope: %w", err)
	}
	if !registered[probe.Type] {
		return Event{}, fmt.Errorf("genui: unregistered event type %q", probe.Type)
	}
	var e Event
	if err := json.Unmarshal(raw, &e); err != nil {
		return Event{}, fmt.Errorf("genui: decode event: %w", err)
	}
	if err := e.validate(); err != nil {
		return Event{}, err
	}
	return e, nil
}

// Emit encodes one event onto a client's SSE Hub channel: the same
// fan-out every ephemeral signal already uses (BroadcastExcept for
// exclude-origin). Delivery is best-effort like every other hub send.
func Emit(hub *queue.SSEHub, clientID string, e Event) error {
	raw, err := Encode(e)
	if err != nil {
		return err
	}
	hub.Send(clientID, raw)
	return nil
}
